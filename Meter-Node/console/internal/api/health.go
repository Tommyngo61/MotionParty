package api

import (
	"net/http"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
)

// liveResponse is the /healthz body.
type liveResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	UptimeS int64  `json:"uptime_s"`
	SchemaV string `json:"schema_version"`
}

// handleLive answers "is this process running". It deliberately does NOT touch
// the database.
//
// Liveness and readiness are different questions and conflating them is how a
// database blip turns into an orchestrator killing every controller replica —
// which drops every agent socket at once, and the whole fleet reconnects
// together. Liveness stays true through a dependency outage; readiness goes
// false so the load balancer stops sending new work.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, liveResponse{
		Status:  "ok",
		Version: s.version,
		UptimeS: int64(time.Since(s.startedAt).Seconds()),
		SchemaV: proto.SchemaSemVer,
	})
}

type readyResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Checks  map[string]string `json:"checks"`
}

// handleReady answers "can this replica serve traffic", which means checking
// the dependencies it cannot work without.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	status, code := "ok", http.StatusOK

	if s.health != nil {
		ctx, cancel := timeoutCtx(r, 3*time.Second)
		defer cancel()
		if err := s.health.Ping(ctx); err != nil {
			checks["database"] = "unavailable: " + err.Error()
			status, code = "degraded", http.StatusServiceUnavailable
		} else {
			checks["database"] = "ok"
		}
	}

	writeJSON(w, code, readyResponse{Status: status, Version: s.version, Checks: checks})
}

// schemaResponse tells an agent (and a human debugging a version-skew problem)
// exactly what this controller will accept.
type schemaResponse struct {
	Version           string `json:"version"`
	EnvelopeVersion   uint8  `json:"envelope_version"`
	MinSupported      uint8  `json:"min_supported_envelope_version"`
	SampleIntervalS   uint16 `json:"sample_interval_s"`
	FlushIntervalS    uint16 `json:"flush_interval_s"`
	HeartbeatInterval uint16 `json:"heartbeat_interval_s"`
	MonthlyBudgetMB   uint32 `json:"monthly_budget_mb"`
}

func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	h := proto.DefaultConfigHints()
	if s.enroll != nil {
		h = s.enroll.ConfigHints()
	}
	writeJSON(w, http.StatusOK, schemaResponse{
		Version:           proto.SchemaSemVer,
		EnvelopeVersion:   proto.SchemaVersion,
		MinSupported:      proto.MinSupportedVersion,
		SampleIntervalS:   h.SampleIntervalS,
		FlushIntervalS:    h.FlushIntervalS,
		HeartbeatInterval: h.HeartbeatIntervalS,
		MonthlyBudgetMB:   h.MonthlyBudgetMB,
	})
}
