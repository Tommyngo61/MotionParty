package meternodeproto

import "fmt"

// Severity ranks an Event.
type Severity uint8

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
	SeverityCritical
)

var severityNames = map[Severity]string{
	SeverityInfo:     "INFO",
	SeverityWarn:     "WARN",
	SeverityError:    "ERROR",
	SeverityCritical: "CRITICAL",
}

func (s Severity) String() string {
	if n, ok := severityNames[s]; ok {
		return n
	}
	return fmt.Sprintf("SEVERITY(%d)", uint8(s))
}

// Valid reports whether s is a severity this schema version defines.
func (s Severity) Valid() bool {
	_, ok := severityNames[s]
	return ok
}

// ParseSeverity converts the wire-stable uppercase name back to a Severity.
func ParseSeverity(s string) (Severity, error) {
	for sev, n := range severityNames {
		if n == s {
			return sev, nil
		}
	}
	return 0, fmt.Errorf("meternode: unknown severity %q", s)
}

// Event is the payload of a KindEvent envelope: a discrete thing that
// happened, as opposed to a sampled value.
//
// Code is the part that matters. Alert rules, dashboards, and runbooks key off
// Code; Message is for humans and may be reworded freely between agent
// releases. Detail carries structured context without inventing a new event
// type for every variation.
type Event struct {
	T        int64             `msgpack:"t"` // unix ms
	Severity Severity          `msgpack:"severity"`
	Code     string            `msgpack:"code"`
	Message  string            `msgpack:"message"`
	Detail   map[string]string `msgpack:"detail,omitempty"`
}

// EventBatch lets an agent flush several events in one envelope. Events are
// rare compared to metrics, but a node coming back from an outage may have a
// handful buffered, and one envelope per event would waste framing.
type EventBatch struct {
	Events []Event `msgpack:"events"`
}

// Stable event codes.
//
// These strings are API. Renaming one breaks every alert rule and runbook that
// references it, so a code is added but never changed. They are grouped by the
// subsystem that emits them.
const (
	// GPU.
	CodeGPUECCError        = "gpu.ecc_error"
	CodeGPUThrottled       = "gpu.throttled"
	CodeGPUFellOffBus      = "gpu.fell_off_bus"
	CodeGPUPCIeDowntrained = "gpu.pcie_downtrained"
	CodeGPUDriverMissing   = "gpu.driver_missing"
	CodeGPUDriverUpgrading = "gpu.driver_upgrading"

	// Host.
	CodeHostThermalThrottle = "host.thermal_throttle"
	CodeHostDiskSMARTFail   = "host.disk_smart_fail"
	CodeHostDiskFull        = "host.disk_full"
	CodeHostClockSkew       = "host.clock_skew"
	CodeHostBooted          = "host.booted"

	// Agent lifecycle.
	CodeAgentStarted       = "agent.started"
	CodeAgentStopping      = "agent.stopping"
	CodeAgentPanic         = "agent.panic"
	CodeAgentCrashLoop     = "agent.crash_loop"
	CodeAgentConfigReload  = "agent.config_reload"
	CodeAgentUpdateStaged  = "agent.update_staged"
	CodeAgentUpdateApplied = "agent.update_applied"
	CodeAgentRolledBack    = "agent.rolled_back"
	CodeCollectorFailed    = "agent.collector_failed"

	// Network and bandwidth.
	CodeNetCapApproaching   = "net.cap_approaching"
	CodeNetCapExceeded      = "net.cap_exceeded"
	CodeNetDegradedSampling = "net.degraded_sampling"
	CodeNetCaptivePortal    = "net.captive_portal"
	CodeNetUpstreamLow      = "net.upstream_low"
	CodeNetReconnected      = "net.reconnected"

	// Enrollment and identity.
	CodeEnrollCompleted    = "enroll.completed"
	CodeEnrollFailed       = "enroll.failed"
	CodeFingerprintChanged = "identity.fingerprint_changed"
	CodeCredentialRevoked  = "identity.credential_revoked"

	// Commands.
	CodeCommandRejected = "command.rejected"
	CodeCommandFailed   = "command.failed"

	// Workload isolation. Emitted in phase 1 by the startup invariant check
	// even though no workloads run yet.
	CodeIsolationInvariantFailed = "isolation.invariant_failed"
)

// Validate performs the cheap checks the ingest gateway runs on a decoded
// event before storing it.
func (e *Event) Validate() error {
	if e.T <= 0 {
		return ErrNoTimestamp
	}
	if !e.Severity.Valid() {
		return fmt.Errorf("meternode: unknown severity %d", uint8(e.Severity))
	}
	if e.Code == "" {
		return fmt.Errorf("meternode: event has no code")
	}
	return nil
}
