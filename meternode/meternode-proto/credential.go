package meternodeproto

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CredentialPrefix tags the credential format so a future mTLS or rotated
// scheme can coexist during a fleet-wide migration.
const CredentialPrefix = "mnc1"

// Credential is the long-lived, controller-signed identity an agent receives
// at enrollment and presents on every connection.
//
// The shared contract allows either a per-node client certificate (mTLS) or a
// long-lived signed credential. This is the second option, with one addition:
// the credential binds a public key the *agent* generated and never sent
// anywhere, and every request carries a fresh signature made with the matching
// private key. That means the credential is not a bearer token — capturing it
// from a log, a proxy, or a support bundle does not let anyone speak as the
// node. See ADR/0004 for why this rather than mTLS in phase 1.
type Credential struct {
	V               uint8  `msgpack:"v"`
	NodeID          string `msgpack:"node_id"`
	SiteID          string `msgpack:"site_id,omitempty"`
	NodePub         []byte `msgpack:"node_pub"` // ed25519 public key, agent-generated
	FingerprintHash string `msgpack:"fp"`
	IssuedAt        int64  `msgpack:"issued_at"`            // unix ms
	ExpiresAt       int64  `msgpack:"expires_at,omitempty"` // unix ms; 0 = no expiry
	KeyID           string `msgpack:"key_id"`               // controller signing key
}

var (
	ErrCredentialMalformed = errors.New("meternode: malformed credential")
	ErrCredentialExpired   = errors.New("meternode: credential has expired")
	ErrCredentialSig       = errors.New("meternode: credential signature does not verify")
	ErrAuthStale           = errors.New("meternode: request authenticator timestamp outside the accepted window")
	ErrAuthSig             = errors.New("meternode: request authenticator does not verify")
)

var b64 = base64.RawURLEncoding

// SigningBytes is the canonical signed form. As with Command, it is built by
// hand so verification never depends on encoder field ordering.
func (c *Credential) SigningBytes() []byte {
	var b strings.Builder
	b.WriteString("meternode-cred-v1\n")
	write := func(s string) { fmt.Fprintf(&b, "%d:%s\n", len(s), s) }
	write(strconv.Itoa(int(c.V)))
	write(c.NodeID)
	write(c.SiteID)
	write(b64.EncodeToString(c.NodePub))
	write(c.FingerprintHash)
	write(strconv.FormatInt(c.IssuedAt, 10))
	write(strconv.FormatInt(c.ExpiresAt, 10))
	write(c.KeyID)
	return []byte(b.String())
}

// Encode signs the credential and renders it as "mnc1.<body>.<sig>".
func (c *Credential) Encode(priv ed25519.PrivateKey) (string, error) {
	return c.EncodeWith(func(msg []byte) []byte { return ed25519.Sign(priv, msg) })
}

// EncodeWith is Encode for callers that do not hold the raw private key.
//
// The controller's signing key may live in a KMS or an HSM, which will sign a
// message but will never hand over an ed25519.PrivateKey. This is the seam
// that keeps that possible without the controller reimplementing the
// credential format.
func (c *Credential) EncodeWith(sign func(message []byte) []byte) (string, error) {
	body, err := Marshal(c)
	if err != nil {
		return "", fmt.Errorf("meternode: encode credential: %w", err)
	}
	sig := sign(c.SigningBytes())
	if len(sig) != ed25519.SignatureSize {
		return "", fmt.Errorf("meternode: signer returned a %d-byte signature, want %d", len(sig), ed25519.SignatureSize)
	}
	return CredentialPrefix + "." + b64.EncodeToString(body) + "." + b64.EncodeToString(sig), nil
}

// ParseCredential decodes a credential WITHOUT verifying it. The caller needs
// the KeyID to look up the right public key, which means parsing has to happen
// first — so this returns an unverified value and callers must follow it with
// Verify. Nothing derived from an unverified credential may reach the
// database.
func ParseCredential(s string) (*Credential, []byte, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 || parts[0] != CredentialPrefix {
		return nil, nil, ErrCredentialMalformed
	}
	body, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: body: %v", ErrCredentialMalformed, err)
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: signature: %v", ErrCredentialMalformed, err)
	}
	var c Credential
	if err := Unmarshal(body, &c); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrCredentialMalformed, err)
	}
	if c.NodeID == "" || len(c.NodePub) != ed25519.PublicKeySize {
		return nil, nil, ErrCredentialMalformed
	}
	return &c, sig, nil
}

// Verify checks the controller's signature and the expiry window.
//
// Revocation is deliberately not checked here: it needs the credential store,
// so it lives in the controller. A credential that verifies here is authentic,
// not necessarily still valid.
func (c *Credential) Verify(pub ed25519.PublicKey, sig []byte, now time.Time) error {
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, c.SigningBytes(), sig) {
		return ErrCredentialSig
	}
	if c.ExpiresAt > 0 && now.UnixMilli() > c.ExpiresAt {
		return ErrCredentialExpired
	}
	return nil
}

// AuthHeader is the header name carrying the per-request proof of possession.
const AuthHeader = "X-MeterNode-Auth"

// CredentialHeader carries the credential itself.
const CredentialHeader = "Authorization"

// AuthSkew is how far a request authenticator's timestamp may be from the
// controller's clock.
//
// It is generous on purpose. Residential machines lose their RTC across power
// cuts and come back minutes or hours off until NTP catches up, and a node
// that cannot authenticate because the homeowner unplugged it is exactly the
// failure this system exists to avoid. Replay inside the window is bounded by
// the window plus the fact that a replayed telemetry frame is idempotent on
// seq.
const AuthSkew = 5 * time.Minute

// AuthSigningBytes is the canonical string a node signs to prove it holds the
// private key for the credential it presented.
//
// Method and path are included so an authenticator captured from a telemetry
// POST cannot be replayed against, say, an enrollment or admin route.
func AuthSigningBytes(nodeID, method, path string, tsMS int64) []byte {
	var b strings.Builder
	b.WriteString("meternode-auth-v1\n")
	write := func(s string) { fmt.Fprintf(&b, "%d:%s\n", len(s), s) }
	write(nodeID)
	write(strings.ToUpper(method))
	write(path)
	write(strconv.FormatInt(tsMS, 10))
	return []byte(b.String())
}

// MakeAuthenticator produces the AuthHeader value: "<unix_ms>.<b64 signature>".
func MakeAuthenticator(nodeID, method, path string, now time.Time, priv ed25519.PrivateKey) string {
	ts := now.UnixMilli()
	sig := ed25519.Sign(priv, AuthSigningBytes(nodeID, method, path, ts))
	return strconv.FormatInt(ts, 10) + "." + b64.EncodeToString(sig)
}

// VerifyAuthenticator checks an AuthHeader value against a credential's bound
// public key.
func VerifyAuthenticator(header, nodeID, method, path string, nodePub ed25519.PublicKey, now time.Time) error {
	tsStr, sigStr, ok := strings.Cut(header, ".")
	if !ok {
		return ErrAuthSig
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return ErrAuthSig
	}
	drift := now.Sub(time.UnixMilli(ts))
	if drift < 0 {
		drift = -drift
	}
	if drift > AuthSkew {
		return fmt.Errorf("%w: %s off", ErrAuthStale, drift.Round(time.Second))
	}
	sig, err := b64.DecodeString(sigStr)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrAuthSig
	}
	if !ed25519.Verify(nodePub, AuthSigningBytes(nodeID, method, path, ts), sig) {
		return ErrAuthSig
	}
	return nil
}
