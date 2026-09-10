package meternodeproto

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CommandKind names a downstream operation the controller can ask an agent to
// perform.
//
// The set is closed. An agent refuses any kind not in its allowlist, logs the
// rejection, and reports it as CodeCommandRejected — so adding a kind here is
// not enough to make a fleet accept it, which is the point.
type CommandKind string

const (
	// Implemented in phase 1.
	CmdPing               CommandKind = "ping"
	CmdSetSampleInterval  CommandKind = "set_sample_interval"
	CmdCollectDiagnostics CommandKind = "collect_diagnostics"
	CmdTailLogs           CommandKind = "tail_logs"
	CmdRestartAgent       CommandKind = "restart_agent"
	CmdUpdateAgent        CommandKind = "update_agent"

	// Declared but NOT implemented in phase 1. The agent knows these names so
	// it can reject them with a specific reason rather than a generic
	// "unknown kind", but executing one is a phase-2 change.
	CmdRunWorkload  CommandKind = "run_workload"
	CmdStopWorkload CommandKind = "stop_workload"
	CmdDrain        CommandKind = "drain"
	CmdRebootHost   CommandKind = "reboot_host"
)

// Phase1Commands is the default agent allowlist: the kinds an agent built at
// phase 1 will actually execute.
var Phase1Commands = []CommandKind{
	CmdPing,
	CmdSetSampleInterval,
	CmdCollectDiagnostics,
	CmdTailLogs,
	CmdRestartAgent,
	CmdUpdateAgent,
}

// DeclaredCommands is every kind the schema knows about, implemented or not.
var DeclaredCommands = append(append([]CommandKind{}, Phase1Commands...),
	CmdRunWorkload, CmdStopWorkload, CmdDrain, CmdRebootHost)

// Declared reports whether k is a kind this schema version names at all.
func (k CommandKind) Declared() bool {
	for _, d := range DeclaredCommands {
		if d == k {
			return true
		}
	}
	return false
}

// ImplementedInPhase1 reports whether k is expected to actually run.
func (k CommandKind) ImplementedInPhase1() bool {
	for _, d := range Phase1Commands {
		if d == k {
			return true
		}
	}
	return false
}

// Command is one signed instruction from the controller to one agent.
//
// NodeID is part of the signed body and is not in the original sketch. It is
// required: without it, a command captured from one node's socket is a valid
// signed command for every node in the fleet. The same reasoning drives
// ExpiresAt — a queued "restart_agent" that lands on a node three days after
// it was issued is a bug, not a delivery success.
type Command struct {
	ID        string            `msgpack:"id"`      // uuid, unique per issue
	NodeID    string            `msgpack:"node_id"` // the only node that may execute this
	Kind      CommandKind       `msgpack:"kind"`
	Args      map[string]string `msgpack:"args,omitempty"`
	IssuedAt  int64             `msgpack:"issued_at"`  // unix ms, controller clock
	ExpiresAt int64             `msgpack:"expires_at"` // unix ms, controller clock
	KeyID     string            `msgpack:"key_id"`     // which controller key signed this
	Signature []byte            `msgpack:"signature"`  // ed25519 over SigningBytes
}

// CommandResult is the payload of a KindCommandResult envelope.
type CommandResult struct {
	CommandID  string `msgpack:"command_id"`
	StartedAt  int64  `msgpack:"started_at"`
	FinishedAt int64  `msgpack:"finished_at"`
	OK         bool   `msgpack:"ok"`
	// Code is a stable machine-readable outcome, e.g. "ok", "rejected",
	// "expired", "unsupported", "timeout".
	Code   string            `msgpack:"code"`
	Detail map[string]string `msgpack:"detail,omitempty"`
	// Output is bounded command output (diagnostics bundle reference, log
	// tail). Large payloads are uploaded out of band, not inlined here — a
	// node on a 5 Mbps upstream cannot afford to ship a log file inside a
	// telemetry frame.
	Output string `msgpack:"output,omitempty"`
}

// Command result codes.
const (
	ResultOK          = "ok"
	ResultRejected    = "rejected"    // not in the agent's allowlist
	ResultExpired     = "expired"     // arrived after expires_at
	ResultUnsupported = "unsupported" // declared but not implemented in this build
	ResultBadSig      = "bad_signature"
	ResultWrongNode   = "wrong_node"
	ResultTimeout     = "timeout"
	ResultFailed      = "failed"
)

// Signing and verification errors.
var (
	ErrBadSignature   = errors.New("meternode: command signature does not verify")
	ErrCommandExpired = errors.New("meternode: command has expired")
	ErrWrongNode      = errors.New("meternode: command is addressed to a different node")
	ErrNotAllowed     = errors.New("meternode: command kind is not in the agent allowlist")
	ErrUnknownKey     = errors.New("meternode: command signed by an unknown key id")
)

// MaxCommandTTL bounds how far ahead a controller may date a command. A
// command that outlives this cannot be issued at all, so a leaked signature
// has a bounded blast radius.
const MaxCommandTTL = 24 * time.Hour

// SigningBytes returns the canonical byte string that is signed and verified.
//
// It is built by hand rather than by serialising the struct because signature
// verification must not depend on the encoder's field ordering, map iteration
// order, or version. Args keys are sorted; every field is length-delimited so
// no combination of values can be made to collide by shifting a delimiter.
func (c *Command) SigningBytes() []byte {
	var b strings.Builder
	b.WriteString("meternode-cmd-v1\n")
	writeField := func(s string) {
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(s)
		b.WriteByte('\n')
	}
	writeField(c.ID)
	writeField(c.NodeID)
	writeField(string(c.Kind))
	writeField(strconv.FormatInt(c.IssuedAt, 10))
	writeField(strconv.FormatInt(c.ExpiresAt, 10))
	writeField(c.KeyID)

	keys := make([]string, 0, len(c.Args))
	for k := range c.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	writeField(strconv.Itoa(len(keys)))
	for _, k := range keys {
		writeField(k)
		writeField(c.Args[k])
	}
	return []byte(b.String())
}

// Sign fills in KeyID and Signature. Controller side.
func (c *Command) Sign(keyID string, priv ed25519.PrivateKey) {
	c.KeyID = keyID
	c.Signature = ed25519.Sign(priv, c.SigningBytes())
}

// Verify checks a command end to end for a given node, at a given time.
//
// The order matters: identity and expiry are checked before the allowlist so
// the agent reports the most specific reason, and the signature is checked
// first so an unsigned attacker cannot learn anything from the error.
func (c *Command) Verify(pub ed25519.PublicKey, nodeID string, now time.Time, allowlist []CommandKind) error {
	if len(c.Signature) != ed25519.SignatureSize || !ed25519.Verify(pub, c.SigningBytes(), c.Signature) {
		return ErrBadSignature
	}
	if c.NodeID != nodeID {
		return fmt.Errorf("%w: addressed to %s", ErrWrongNode, c.NodeID)
	}
	if c.ExpiresAt > 0 && now.UnixMilli() > c.ExpiresAt {
		return fmt.Errorf("%w: expired at %d, now %d", ErrCommandExpired, c.ExpiresAt, now.UnixMilli())
	}
	for _, k := range allowlist {
		if k == c.Kind {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotAllowed, c.Kind)
}
