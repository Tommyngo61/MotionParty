package meternodeproto

// Enrollment runs over plain HTTPS + JSON rather than the MessagePack used for
// telemetry. It happens once per node, often with a field tech standing in a
// homeowner's utility room reading a terminal, so being able to curl it and
// read the answer is worth more than the handful of bytes msgpack would save.

// EnrollPath is the enrollment endpoint.
const EnrollPath = "/v1/enroll"

// StreamPath is the long-lived WebSocket endpoint.
const StreamPath = "/v1/stream"

// TelemetryPath is the HTTP batch fallback, used when a middlebox or captive
// portal will not keep a WebSocket open.
const TelemetryPath = "/v1/telemetry"

// GPUInventory is one GPU as recorded at enrollment: static facts, not
// telemetry. Live values ride in Sample.GPUs.
type GPUInventory struct {
	Index         uint8  `json:"index"`
	UUID          string `json:"uuid"`
	Name          string `json:"name"`
	MemTotalMB    uint32 `json:"mem_total_mb"`
	DriverVersion string `json:"driver_version"`
	PCIBusID      string `json:"pci_bus_id,omitempty"`
}

// DiskInventory is one block device as recorded at enrollment.
type DiskInventory struct {
	Device     string `json:"device"`
	Model      string `json:"model,omitempty"`
	Serial     string `json:"serial,omitempty"`
	SizeGB     uint32 `json:"size_gb"`
	Rotational *bool  `json:"rotational,omitempty"`
}

// HardwareInventory is the static machine record captured once at enrollment.
// It is what an operator looks at to answer "what is actually in that box"
// without waiting for a live sample from a node that may be unplugged.
type HardwareInventory struct {
	Hostname     string          `json:"hostname"`
	CPUModel     string          `json:"cpu_model"`
	CPUCores     uint16          `json:"cpu_cores"`
	CPUThreads   uint16          `json:"cpu_threads"`
	MemTotalMB   uint32          `json:"mem_total_mb"`
	Kernel       string          `json:"kernel"`
	OS           string          `json:"os"`
	BoardVendor  string          `json:"board_vendor,omitempty"`
	BoardProduct string          `json:"board_product,omitempty"`
	GPUs         []GPUInventory  `json:"gpus"`
	Disks        []DiskInventory `json:"disks,omitempty"`
}

// EnrollRequest is the body of POST /v1/enroll.
type EnrollRequest struct {
	// Token is the one-time, short-lived, single-use enrollment token minted
	// by the controller and either baked into the provisioning image or typed
	// in by the field tech.
	Token string `json:"token"`

	// NodePub is an ed25519 public key the agent generated locally. The
	// private half never leaves the node — not at enrollment, not ever.
	NodePub []byte `json:"node_pub"`

	Fingerprint  HardwareFingerprint `json:"fingerprint"`
	Hardware     HardwareInventory   `json:"hardware"`
	AgentVersion string              `json:"agent_version"`

	// PriorCredential is the credential this node already holds, if any.
	//
	// It is what makes a hardware change distinguishable from a new machine.
	// Without it, a node whose GPU was swapped presents an unrecognised
	// fingerprint and looks exactly like a box that arrived from the
	// warehouse this morning — and the controller would enrol it as a second
	// identity for the same physical machine. The agent sends whatever
	// credential it has whenever it re-enrols; the controller treats it as a
	// claim of identity to be verified, never as authorisation.
	PriorCredential string `json:"prior_credential,omitempty"`

	// SentAtMS is the agent's clock at request time. The controller compares
	// it with its own to seed the node's clock-skew record — a node that
	// enrolls two hours off is worth knowing about before its first sample
	// lands in a hypertable.
	SentAtMS int64 `json:"sent_at_ms"`
}

// EnrollResponse is the 200 body of POST /v1/enroll.
type EnrollResponse struct {
	NodeID     string `json:"node_id"`
	SiteID     string `json:"site_id,omitempty"`
	Credential string `json:"credential"`

	// ControllerKeys are the controller's command-signing public keys, by key
	// id. The agent pins these at enrollment and verifies every downstream
	// command against them; more than one is returned so a signing key can be
	// rotated without a flag day across a fleet that is half offline.
	ControllerKeys map[string][]byte `json:"controller_keys"`

	// Config is the server-chosen operating envelope. The agent has defaults
	// for all of it and works without this, but the controller gets to move
	// the fleet's intervals without shipping a new binary.
	Config AgentConfigHints `json:"config"`

	// ServerTimeMS lets the agent compute its clock skew immediately rather
	// than waiting for the first heartbeat round trip.
	ServerTimeMS int64 `json:"server_time_ms"`
}

// AgentConfigHints are controller-supplied operating parameters. Defaults come
// from the shared bandwidth budget in the contract.
type AgentConfigHints struct {
	SampleIntervalS    uint16 `json:"sample_interval_s"`    // 10
	FlushIntervalS     uint16 `json:"flush_interval_s"`     // 60
	HeartbeatIntervalS uint16 `json:"heartbeat_interval_s"` // 15
	StreamURL          string `json:"stream_url"`
	TelemetryURL       string `json:"telemetry_url"`
	// MonthlyBudgetMB is the control-plane byte budget the agent degrades
	// against. Target 150, hard ceiling 500.
	MonthlyBudgetMB uint32 `json:"monthly_budget_mb"`
}

// DefaultConfigHints returns the contract's budgeted defaults.
func DefaultConfigHints() AgentConfigHints {
	return AgentConfigHints{
		SampleIntervalS:    10,
		FlushIntervalS:     60,
		HeartbeatIntervalS: 15,
		MonthlyBudgetMB:    150,
	}
}

// APIError is the uniform error body for every non-2xx JSON response.
// Code is stable and machine-readable; Message is for the field tech.
type APIError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Detail  map[string]string `json:"detail,omitempty"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Enrollment error codes.
const (
	ErrCodeTokenInvalid        = "token_invalid"
	ErrCodeTokenExpired        = "token_expired"
	ErrCodeTokenConsumed       = "token_consumed"
	ErrCodeTokenRevoked        = "token_revoked"
	ErrCodeFingerprintConflict = "fingerprint_conflict"
	ErrCodeReattestRequired    = "reattestation_required"
	ErrCodeNodeRevoked         = "node_revoked"
	ErrCodeNodeQuarantined     = "node_quarantined"
	ErrCodeBadRequest          = "bad_request"
	ErrCodeUnauthorized        = "unauthorized"
	ErrCodeInternal            = "internal"
)

// ReattestationRequired is the 409 body when an already-enrolled node presents
// a fingerprint that does not match the one bound to its identity.
//
// This is never resolved automatically. A changed fingerprint means either a
// hardware change we should know about, or an image that has been cloned onto
// a second machine — and silently re-enrolling would turn the second case into
// two nodes sharing an identity, which is the worst outcome available.
type ReattestationRequired struct {
	NodeID          string   `json:"node_id"`
	ChangedFields   []string `json:"changed_fields"`
	BoundHash       string   `json:"bound_fingerprint_hash"`
	PresentedHash   string   `json:"presented_fingerprint_hash"`
	ReattestationID string   `json:"reattestation_id"`
	Message         string   `json:"message"`
}
