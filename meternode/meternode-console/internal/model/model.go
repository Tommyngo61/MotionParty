// Package model holds the controller's persistent domain types.
//
// It is deliberately dependency-free apart from uuid: every other package
// depends on it, and it depends on nothing, so nothing here can drag a
// database driver or an HTTP router into a unit test.
package model

import (
	"time"

	"github.com/google/uuid"
	proto "github.com/meterhome/meternode-proto"
)

// NodeLifecycle mirrors the node_lifecycle enum.
//
// This is the ADMINISTRATIVE state an operator sets. It is not the health
// state — that is derived from telemetry by the health scorer and is never
// stored as something the agent asserts. Keeping the two apart is deliberate:
// an operator can quarantine a perfectly healthy node, and an enrolled node can
// be reporting ECC errors.
type NodeLifecycle string

const (
	LifecycleUnenrolled  NodeLifecycle = "unenrolled"
	LifecycleEnrolled    NodeLifecycle = "enrolled"
	LifecycleQuarantined NodeLifecycle = "quarantined"
	LifecycleRetired     NodeLifecycle = "retired"
)

// Site is a residence hosting one or more nodes.
type Site struct {
	ID           uuid.UUID
	ShortName    string
	Name         string
	Timezone     string
	ISP          string
	PlanDownMbps int
	PlanUpMbps   int
	MonthlyCapGB int
	CreatedAt    time.Time
}

// Node is one company-owned machine in a residence.
type Node struct {
	ID               uuid.UUID
	SiteID           *uuid.UUID
	Name             string
	Lifecycle        NodeLifecycle
	AgentVersion     string
	EnrolledAt       *time.Time
	LastSeenAt       *time.Time
	LastSeq          uint64
	QuarantineReason string
	CreatedAt        time.Time
}

// Hardware is the fingerprint and inventory bound to a node identity.
type Hardware struct {
	NodeID              uuid.UUID
	FingerprintHash     string
	MotherboardUUID     string
	GPUUUIDs            []string
	PrimaryNICMAC       string
	FingerprintComplete bool
	Inventory           proto.HardwareInventory
	BoundAt             time.Time
}

// Token is a one-time enrollment token. The raw token value exists exactly
// once, in the response to the mint call; only its hash is ever persisted.
type Token struct {
	ID             uuid.UUID
	TokenHash      []byte
	TokenPrefix    string
	Label          string
	SiteID         *uuid.UUID
	NodeNameHint   string
	CreatedBy      string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
	ConsumedByNode *uuid.UUID
	RevokedAt      *time.Time
	RevokedBy      string
	RevokeReason   string
}

// Usable reports whether a token may still be exchanged for a credential.
func (t *Token) Usable(now time.Time) bool {
	return t.ConsumedAt == nil && t.RevokedAt == nil && now.Before(t.ExpiresAt)
}

// Credential is an issued node identity, tracked for revocation. The credential
// itself is self-contained and signed; this row exists so it can be cancelled.
type Credential struct {
	ID              uuid.UUID
	NodeID          uuid.UUID
	KeyID           string
	NodePub         []byte
	FingerprintHash string
	CredentialSHA   []byte
	IssuedAt        time.Time
	ExpiresAt       *time.Time
	RevokedAt       *time.Time
	RevokedBy       string
	RevokeReason    string
}

// ReattestationState mirrors the reattestation_state enum.
type ReattestationState string

const (
	ReattestPending  ReattestationState = "pending"
	ReattestApproved ReattestationState = "approved"
	ReattestRejected ReattestationState = "rejected"
)

// Reattestation records an enrolled node presenting hardware that does not
// match what its identity is bound to. It is resolved by a human, never
// automatically — see enroll.Service.Enroll for why.
type Reattestation struct {
	ID                 uuid.UUID
	NodeID             uuid.UUID
	State              ReattestationState
	BoundHash          string
	PresentedHash      string
	ChangedFields      []string
	PresentedInventory proto.HardwareInventory
	PresentedFromIP    string
	CreatedAt          time.Time
	ResolvedAt         *time.Time
	ResolvedBy         string
	ResolutionNote     string
}

// Event is a stored node event, whether reported by the agent or derived by
// the controller.
type Event struct {
	ID       int64
	NodeID   uuid.UUID
	T        time.Time
	Severity string
	Code     string
	Message  string
	Detail   map[string]string
	Source   string // "agent" | "controller"
}

// AuditEntry records an operator-initiated action. Append-only: nothing in the
// application updates or deletes one.
type AuditEntry struct {
	At         time.Time
	Actor      string
	ActorRole  string
	Action     string
	NodeID     *uuid.UUID
	SiteID     *uuid.UUID
	CommandID  *uuid.UUID
	Target     string
	Result     string // "ok" | "denied" | "error"
	Detail     map[string]string
	RequestID  string
	RemoteAddr string
}

// Audit action verbs. Stable strings: the audit view filters on them and
// someone will eventually build a compliance report from them.
const (
	ActionTokenMint       = "enroll_token.mint"
	ActionTokenRevoke     = "enroll_token.revoke"
	ActionTokenList       = "enroll_token.list"
	ActionNodeEnroll      = "node.enroll"
	ActionNodeReEnroll    = "node.re_enroll"
	ActionNodeQuarantine  = "node.quarantine"
	ActionNodeRelease     = "node.release"
	ActionCredRevoke      = "credential.revoke"
	ActionReattestOpen    = "reattestation.open"
	ActionReattestApprove = "reattestation.approve"
	ActionReattestReject  = "reattestation.reject"
	ActionCommandIssue    = "command.issue"
	ActionReleasePublish  = "agent_release.publish"
)

// ControllerKey is a controller command-signing key.
//
// More than one may be active: a fleet is always partly offline, so rotating a
// signing key means both keys verify for as long as the slowest node takes to
// come back and pin the new set.
type ControllerKey struct {
	KeyID      string
	PublicKey  []byte
	PrivateKey []byte // dev/compose only; production supplies the seed out of band
	Active     bool
	CreatedAt  time.Time
	RetiredAt  *time.Time
}

// OperatorRole mirrors the operator_role enum.
type OperatorRole string

const (
	RoleViewer   OperatorRole = "viewer"
	RoleOperator OperatorRole = "operator"
	RoleAdmin    OperatorRole = "admin"
)

// AtLeast reports whether r carries the authority of want. Roles are a total
// order, so this is a comparison rather than a permission matrix — a matrix is
// what phase 2 needs, when workload placement gives operators things to do that
// admins should not do by accident.
func (r OperatorRole) AtLeast(want OperatorRole) bool {
	rank := map[OperatorRole]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}
	return rank[r] >= rank[want] && rank[r] > 0
}
