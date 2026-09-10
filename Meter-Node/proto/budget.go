package meternodeproto

import "time"

// The control-plane bandwidth budget from the shared contract.
//
// These are not advisory. The agent enforces them at runtime and the test
// suite in both repos asserts against them, because the entire premise of
// putting company hardware in someone's house is that our traffic is a
// rounding error against their ISP's ~1.2 TB monthly cap — and that some of
// those ISPs prohibit commercial use, so "looks like a normal heavy consumer
// device" is a design requirement rather than a preference.
const (
	// DefaultSampleInterval is the metric sampling resolution.
	DefaultSampleInterval = 10 * time.Second
	// DefaultFlushInterval is how often a compressed batch goes up.
	DefaultFlushInterval = 60 * time.Second
	// DefaultHeartbeatInterval is the liveness beat.
	DefaultHeartbeatInterval = 15 * time.Second

	// HeartbeatMaxBytes caps one encoded heartbeat envelope on the wire.
	HeartbeatMaxBytes = 100

	// TargetMonthlyBytes is what a node should actually use.
	TargetMonthlyBytes = 150 * 1024 * 1024
	// CeilingMonthlyBytes is the hard limit. Crossing it is a CRITICAL event
	// and forces the agent into its most degraded telemetry mode.
	CeilingMonthlyBytes = 500 * 1024 * 1024

	// DegradeAtFraction is the share of the monthly budget at which the agent
	// starts lengthening intervals and emits CodeNetCapApproaching. It is
	// deliberately well below 1.0: degrading at 99% would mean the last day
	// of every month is a telemetry blackout.
	DegradeAtFraction = 0.75
)

// AverageMonth is the accounting month used for budget maths. Residential
// caps reset on a billing date, not on the calendar, so a fixed 30-day figure
// keeps the agent's own accounting and the controller's projection agreeing
// with each other rather than each being differently wrong.
const AverageMonth = 30 * 24 * time.Hour

// ProjectedMonthlyBytes extrapolates a monthly total from bytes observed over
// a window. Returns 0 for a non-positive window rather than dividing by zero.
func ProjectedMonthlyBytes(bytes uint64, window time.Duration) uint64 {
	if window <= 0 {
		return 0
	}
	return uint64(float64(bytes) * (float64(AverageMonth) / float64(window)))
}

// BudgetStatus classifies a node's month-to-date control-plane usage.
type BudgetStatus uint8

const (
	BudgetOK BudgetStatus = iota
	BudgetApproaching
	BudgetOverTarget
	BudgetOverCeiling
)

func (b BudgetStatus) String() string {
	switch b {
	case BudgetOK:
		return "ok"
	case BudgetApproaching:
		return "approaching"
	case BudgetOverTarget:
		return "over_target"
	case BudgetOverCeiling:
		return "over_ceiling"
	default:
		return "unknown"
	}
}

// ClassifyBudget maps month-to-date control-plane bytes against a node's
// configured budget. budgetBytes of 0 means use the default target.
func ClassifyBudget(mtdBytes, budgetBytes uint64) BudgetStatus {
	if budgetBytes == 0 {
		budgetBytes = TargetMonthlyBytes
	}
	switch {
	case mtdBytes >= CeilingMonthlyBytes:
		return BudgetOverCeiling
	case mtdBytes >= budgetBytes:
		return BudgetOverTarget
	case float64(mtdBytes) >= float64(budgetBytes)*DegradeAtFraction:
		return BudgetApproaching
	default:
		return BudgetOK
	}
}
