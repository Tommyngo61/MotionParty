// Package version carries build identity.
//
// It is its own package because both binaries and the telemetry payload need
// it, and a variable in main is not reachable from a collector.
package version

// Version is set at build time: -ldflags "-X .../internal/version.Version=1.2.3".
//
// It rides in every metric sample's host block and in the enrollment request,
// so the controller can answer "which build is this fleet running" without a
// separate inventory call — which matters for a staged rollout that has to halt
// when the canary cohort regresses.
var Version = "dev"

// Commit is the source revision, for support bundles.
var Commit = "unknown"

// BuildDate is when the binary was produced.
var BuildDate = "unknown"

// String renders the full build identity for `--version` and log lines.
func String() string {
	return Version + " (" + Commit + ", built " + BuildDate + ")"
}
