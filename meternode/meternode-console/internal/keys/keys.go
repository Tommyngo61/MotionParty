// Package keys holds the controller's command-signing identity.
//
// The controller signs two things with this key: node credentials issued at
// enrollment, and every downstream command. Agents pin the public half at
// enrollment and verify against it forever, which makes the key's continuity
// across restarts a fleet-wide correctness property rather than an operational
// detail — a controller that mints a new key on boot has silently told every
// enrolled node to reject its commands.
package keys

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
)

// Signer is an ed25519 signer over a set of keys.
//
// It holds more than one public key on purpose. A fleet is always partly
// offline, so rotating a signing key means both the old and new keys must
// verify for as long as the slowest node takes to come back and pin the new
// set. Signing always uses the active key; verification accepts any.
type Signer struct {
	mu      sync.RWMutex
	keyID   string
	priv    ed25519.PrivateKey
	publics map[string][]byte
}

var ErrNoKey = errors.New("keys: no signing key configured")

// FromSeed builds a Signer from a base64 (standard or raw-URL) ed25519 seed.
func FromSeed(keyID, seedB64 string) (*Signer, error) {
	if keyID == "" {
		return nil, errors.New("keys: key id is required")
	}
	seed, err := decodeB64(seedB64)
	if err != nil {
		return nil, fmt.Errorf("keys: decode seed: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("keys: seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return newSigner(keyID, priv), nil
}

// Generate mints a throwaway key. Dev only — cmd/controller refuses to call it
// outside dev, and config.Validate refuses the configuration that would let it.
func Generate(keyID string) (*Signer, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("keys: generate: %w", err)
	}
	return newSigner(keyID, priv), nil
}

func newSigner(keyID string, priv ed25519.PrivateKey) *Signer {
	pub := append([]byte(nil), priv.Public().(ed25519.PublicKey)...)
	return &Signer{keyID: keyID, priv: priv, publics: map[string][]byte{keyID: pub}}
}

// KeyID returns the active signing key's id.
func (s *Signer) KeyID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keyID
}

// Sign signs message with the active key.
func (s *Signer) Sign(message []byte) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.priv == nil {
		return nil
	}
	return ed25519.Sign(s.priv, message)
}

// PublicKeys returns every key an agent should pin, active and retired-but-
// still-trusted, as a copy.
func (s *Signer) PublicKeys() map[string][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]byte, len(s.publics))
	for k, v := range s.publics {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

// TrustRetired adds a previously-active public key so credentials and commands
// it signed keep verifying through a rotation.
func (s *Signer) TrustRetired(keyID string, pub []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("keys: public key must be %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if keyID == s.keyID {
		return errors.New("keys: cannot retire the active key")
	}
	s.publics[keyID] = append([]byte(nil), pub...)
	return nil
}

// PublicKey returns the public half of a specific key id.
func (s *Signer) PublicKey(keyID string) (ed25519.PublicKey, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pub, ok := s.publics[keyID]
	if !ok {
		return nil, false
	}
	return ed25519.PublicKey(pub), true
}

// Seed returns the active key's seed, base64 raw-URL encoded. Used only by the
// dev bootstrap, which persists a generated key so restarts do not invalidate
// agents that already pinned it.
func (s *Signer) Seed() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.priv == nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(s.priv.Seed())
}

func decodeB64(v string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(v); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not valid base64")
}
