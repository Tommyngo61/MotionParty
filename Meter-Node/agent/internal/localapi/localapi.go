// Package localapi is the agent's local diagnostic interface.
//
// It is a unix socket. Not a unix socket by default, not a unix socket unless
// configured otherwise — a unix socket, full stop. The node shares a LAN with
// the homeowner's laptop, phone, TV, and whatever else is plugged in, and none
// of those may be able to reach anything the agent exposes. There is no
// configuration in this codebase that opens a listening TCP port on a node, and
// adding one would be a security regression, not a feature.
//
// The audience is a field tech standing at the house with a laptop on the
// node's console, asking "why isn't this thing working". Everything here is
// shaped by that: read-only, no authentication beyond filesystem permissions,
// and answers that say what to do next.
package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// SocketMode is the permission on the socket file.
//
// 0660, group-owned by the agent's group. Filesystem permissions ARE the
// authentication: a tech runs meternodectl as a member of that group (or via
// sudo), and nothing else on the box can read it. 0666 would let any process on
// the node — including, once workloads exist, a customer container that
// escaped its mount namespace — read the node's status.
const SocketMode os.FileMode = 0o660

// Status is what `meternodectl status` renders.
//
// It is deliberately flat and deliberately complete: a tech reading this over a
// phone should not have to run three commands to work out whether the node is
// enrolled, whether it is connected, and when it last managed to ship anything.
type Status struct {
	AgentVersion string `json:"agent_version"`
	StartedAt    string `json:"started_at"`
	UptimeS      int64  `json:"uptime_s"`
	PID          int    `json:"pid"`

	// Identity.
	Enrolled      bool   `json:"enrolled"`
	NodeID        string `json:"node_id,omitempty"`
	SiteID        string `json:"site_id,omitempty"`
	ControllerURL string `json:"controller_url,omitempty"`
	EnrollHint    string `json:"enroll_hint,omitempty"`

	// Transport (M3). Reported now so the shape does not change under the
	// tooling later.
	Connected     bool   `json:"connected"`
	Transport     string `json:"transport,omitempty"` // websocket | http-batch | none
	LastConnectAt string `json:"last_connect_at,omitempty"`
	LastUploadAt  string `json:"last_upload_at,omitempty"`
	ReconnectIn   string `json:"reconnect_in,omitempty"`
	ClockSkewMS   int64  `json:"clock_skew_ms"`

	// Collection.
	Collectors        []string `json:"collectors"`
	FailingCollectors []string `json:"failing_collectors,omitempty"`
	GPUSource         string   `json:"gpu_source,omitempty"`
	SampleIntervalS   int      `json:"sample_interval_s"`
	LastSampleAt      string   `json:"last_sample_at,omitempty"`
	LastSampleMS      int64    `json:"last_sample_duration_ms"`
	SamplesTaken      uint64   `json:"samples_taken"`

	// Buffer.
	Buffered       int    `json:"buffered_samples"`
	BufferCapacity int    `json:"buffer_capacity"`
	BufferDropped  uint64 `json:"buffer_dropped"`

	// Bandwidth (M4).
	ControlPlaneBytesMTD uint64 `json:"control_plane_bytes_mtd"`
	BudgetMB             uint32 `json:"monthly_budget_mb"`
	BudgetStatus         string `json:"budget_status"`

	// Resilience.
	BootCount  uint64 `json:"boot_count"`
	CrashCount uint64 `json:"crash_count"`

	// Warnings are the things a tech should read first: not enrolled, no
	// driver, cannot reach the controller.
	Warnings []string `json:"warnings,omitempty"`
}

// StatusFunc produces the current status.
type StatusFunc func() Status

// SampleFunc returns the most recent sample, for `meternodectl sample`.
type SampleFunc func() any

// Server serves the local API over a unix socket.
type Server struct {
	path   string
	log    *slog.Logger
	status StatusFunc
	sample SampleFunc

	listener net.Listener
	http     *http.Server
}

// New builds the server.
func New(path string, log *slog.Logger, status StatusFunc, sample SampleFunc) *Server {
	return &Server{path: path, log: log, status: status, sample: sample}
}

// Start creates the socket and begins serving.
func (s *Server) Start() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("localapi: create socket directory: %w", err)
	}
	// A stale socket from an unclean shutdown (a power cut, which on these
	// machines is routine) would otherwise make the agent fail to start with
	// "address already in use" — on a node nobody can log into.
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("localapi: remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("localapi: listen on %s: %w", s.path, err)
	}
	if err := os.Chmod(s.path, SocketMode); err != nil {
		ln.Close()
		return fmt.Errorf("localapi: set socket permissions: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/sample", s.handleSample)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	s.listener = ln
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("local api stopped", "error", err)
		}
	}()

	s.log.Info("local diagnostic socket ready", "path", s.path, "mode", SocketMode.String())
	return nil
}

// Stop shuts the server down and removes the socket.
func (s *Server) Stop(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	err := s.http.Shutdown(ctx)
	_ = os.Remove(s.path)
	return err
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.status())
}

func (s *Server) handleSample(w http.ResponseWriter, r *http.Request) {
	if s.sample == nil {
		http.Error(w, "no sample available yet", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, s.sample())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
