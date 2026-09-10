// Package meternodeproto defines the MeterNode wire schema shared by the
// MeterNode Console (controller) and the MeterNode Agent.
//
// This package is the single source of truth for anything that crosses the
// network between an agent and the controller. Both sides import it; neither
// side hand-rolls a struct that goes on the wire.
//
// # Compatibility rules
//
// These rules are load-bearing. Nodes live in homeowners' residences and
// update on their own schedule, so the controller always talks to a spread of
// agent versions at once.
//
//   - Every metric name is stable and snake_case.
//   - Adding a field is a MINOR bump. Removing or retyping a field is a MAJOR
//     bump.
//   - The controller MUST accept envelopes from agents that are up to
//     MinSupportedVersion behind, which is at least two releases.
//
// See ADR/0002-wire-schema.md in meternode-console for the reasoning behind
// MessagePack + zstd rather than protobuf.
package meternodeproto

// SchemaVersion is the envelope schema version this build emits.
//
// It is deliberately a uint8: it rides in every single envelope, including
// the sub-100-byte heartbeat, so it may not grow.
const SchemaVersion uint8 = 1

// MinSupportedVersion is the oldest envelope version a controller built from
// this package must still accept. Raising it is a breaking change for any
// node that has not yet self-updated, so it may only move once the fleet's
// minimum agent version has been verified above it.
const MinSupportedVersion uint8 = 1

// SchemaSemVer is the human-facing version of the schema, for release notes
// and the /v1/schema endpoint. It tracks the minor/major rules above.
const SchemaSemVer = "1.0.0"

// SupportsVersion reports whether a controller built against this package can
// decode an envelope stamped with version v.
//
// Newer-than-known versions are rejected rather than best-effort decoded: an
// agent that is ahead of its controller is a rollout bug, and silently
// dropping fields we do not understand would hide it.
func SupportsVersion(v uint8) bool {
	return v >= MinSupportedVersion && v <= SchemaVersion
}
