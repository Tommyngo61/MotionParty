package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	proto "github.com/meterhome/meternode-proto"

	"github.com/meterhome/meternode-agent/internal/config"
	"github.com/meterhome/meternode-agent/internal/localapi"
	"github.com/meterhome/meternode-agent/internal/supervise"
)

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func testAgent(t *testing.T, mut func(*config.Config)) (*Agent, *config.Config) {
	t.Helper()
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Paths.StateDir = dir
	cfg.Paths.SocketPath = filepath.Join(dir, "agent.sock")
	cfg.Collect.SampleInterval = 50 * time.Millisecond
	cfg.Collect.FlushInterval = 200 * time.Millisecond
	cfg.Collect.CollectorTimeout = 20 * time.Millisecond
	if mut != nil {
		mut(&cfg)
	}

	boot, err := supervise.LoadBootCounter(filepath.Join(dir, "boot-counter"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(Options{Config: &cfg, Logger: log, Boot: boot}), &cfg
}

func TestAgentSamplesImmediately(t *testing.T) {
	// A tech who has just started the agent and runs `meternodectl status`
	// should see real data, not an empty struct.
	a, _ := testAgent(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(30 * time.Millisecond) // less than one sample interval
	st := a.Status(false, "", "")
	if st.SamplesTaken == 0 {
		t.Fatal("the agent must take a sample at start, not after one interval")
	}
	<-done
}

func TestAgentKeepsSamplingIntoTheRing(t *testing.T) {
	a, _ := testAgent(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	<-done

	if got := a.Buffer().Len(); got < 3 {
		t.Fatalf("expected several buffered samples, got %d", got)
	}
	st := a.Status(false, "", "")
	if st.SamplesTaken < 3 {
		t.Fatalf("samples taken = %d, want at least 3", st.SamplesTaken)
	}
	if st.LastSampleAt == "" {
		t.Error("status must report when the last sample happened")
	}
}

// TestReloadChangesTheSampleIntervalImmediately: a SIGHUP that lengthened the
// interval to conserve bandwidth must take effect now, not at the next tick of
// the OLD interval — which could be minutes away.
func TestReloadChangesTheSampleIntervalImmediately(t *testing.T) {
	a, cfg := testAgent(t, func(c *config.Config) {
		c.Collect.SampleInterval = 2 * time.Second
		c.Collect.FlushInterval = 4 * time.Second
		c.Collect.CollectorTimeout = 500 * time.Millisecond
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	before := a.Status(false, "", "").SamplesTaken

	next := *cfg
	next.Collect.SampleInterval = 40 * time.Millisecond
	next.Collect.CollectorTimeout = 20 * time.Millisecond
	a.Reload(&next)

	time.Sleep(250 * time.Millisecond)
	after := a.Status(false, "", "").SamplesTaken

	cancel()
	<-done

	if after-before < 3 {
		t.Fatalf("only %d samples in 250ms after shortening the interval to 40ms; the reload did not take effect", after-before)
	}
}

// TestHealthyGoesFalseWhenSamplingStalls is what the systemd watchdog reads.
// An agent whose sampling loop has stalled is still a running process, still
// answering its socket, and completely useless.
func TestHealthyGoesFalseWhenSamplingStalls(t *testing.T) {
	a, _ := testAgent(t, nil)
	if !a.Healthy() {
		t.Fatal("a freshly built agent should be healthy")
	}

	// Backdate the last progress past the threshold without running the loop.
	a.mu.Lock()
	a.lastProgress = time.Now().Add(-2 * time.Minute)
	a.mu.Unlock()

	if a.Healthy() {
		t.Fatal("an agent that has not sampled in two minutes must report unhealthy")
	}
}

// TestStatusWarnsAboutTheThingsAFieldTechNeeds. The warnings are the first
// thing rendered by meternodectl, so the important ones must be there.
func TestStatusWarnsAboutUnenrolledAndMissingDriver(t *testing.T) {
	a, _ := testAgent(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	<-done

	st := a.Status(false, "", "")
	var sawEnroll bool
	for _, w := range st.Warnings {
		if contains(w, "not enrolled") {
			sawEnroll = true
		}
	}
	if !sawEnroll {
		t.Errorf("an unenrolled node must say so first: %v", st.Warnings)
	}
	if st.EnrollHint == "" {
		t.Error("status must tell the tech where to put the enrollment token")
	}
	// The test host has no NVIDIA driver, so this exercises the real degraded
	// path rather than a stub.
	if st.GPUSource != "none" {
		t.Logf("this host has a GPU source (%s); skipping the no-driver assertion", st.GPUSource)
	} else {
		var sawDriver bool
		for _, w := range st.Warnings {
			if contains(w, "NVIDIA driver") {
				sawDriver = true
			}
		}
		if !sawDriver {
			t.Errorf("a node with no driver must say so: %v", st.Warnings)
		}
	}
}

// TestLocalSocketOnly: there is no configuration in this codebase that opens a
// listening TCP port on a node, and the socket must not be world-readable.
func TestLocalSocketPermissionsAndType(t *testing.T) {
	a, cfg := testAgent(t, nil)
	log := slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := localapi.New(cfg.Paths.SocketPath, log,
		func() localapi.Status { return a.Status(false, "", "") },
		func() any { return a.LastSample() })
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())

	info, err := os.Stat(cfg.Paths.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatal("the local interface must be a unix socket")
	}
	if perm := info.Mode().Perm(); perm != localapi.SocketMode {
		t.Fatalf("socket mode = %v, want %v — filesystem permissions ARE the authentication here", perm, localapi.SocketMode)
	}
	if perm := info.Mode().Perm(); perm&0o004 != 0 {
		t.Fatal("the socket must not be world-readable")
	}

	client := localapi.NewClient(cfg.Paths.SocketPath)
	st, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("the client must be able to read status: %v", err)
	}
	if st.PID == 0 {
		t.Error("status should carry the agent pid")
	}
}

// TestStaleSocketIsReplaced: a power cut leaves a socket file behind, and the
// agent must not then fail to start with "address already in use" on a node
// nobody can log into.
func TestStaleSocketIsReplaced(t *testing.T) {
	a, cfg := testAgent(t, nil)
	if err := os.WriteFile(cfg.Paths.SocketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := localapi.New(cfg.Paths.SocketPath, log,
		func() localapi.Status { return a.Status(false, "", "") }, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("a stale socket file must not stop the agent starting: %v", err)
	}
	_ = srv.Stop(context.Background())
}

func TestSampleEndpointReturnsRealTelemetry(t *testing.T) {
	a, cfg := testAgent(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	<-done

	log := slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := localapi.New(cfg.Paths.SocketPath, log,
		func() localapi.Status { return a.Status(false, "", "") },
		func() any { return a.LastSample() })
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())

	raw, err := localapi.NewClient(cfg.Paths.SocketPath).Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var s proto.Sample
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.CPU == nil || s.Mem == nil || s.Host == nil {
		t.Fatalf("the sample should carry host telemetry: %+v", s)
	}
	// The test host has no GPU, so this is the "absent is not zero" case
	// (ADR 0003) exercised against a real machine.
	if s.GPUs != nil && len(s.GPUs) == 0 {
		t.Error("an absent GPU must be nil, not an empty slice")
	}
	if s.Mem.TotalMB == 0 {
		t.Error("memory total should be a real reading")
	}
}

func TestClientErrorsAreWrittenForAFieldTech(t *testing.T) {
	// The most likely thing a tech sees is "the agent isn't running". The
	// error must say that, and say what to run next.
	c := localapi.NewClient(filepath.Join(t.TempDir(), "nothing.sock"))
	_, err := c.Status(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !contains(err.Error(), "not running") || !contains(err.Error(), "systemctl") {
		t.Fatalf("the error must tell a tech what to do, got %q", err)
	}
}

// TestEventQueuePrefersDroppingInfoEvents: an hour of "collector recovered"
// notices must not push out the one CRITICAL that explains a node's failure.
func TestEventQueuePrefersDroppingInfoEvents(t *testing.T) {
	q := NewEventQueue(3)
	q.Push(proto.Event{Severity: proto.SeverityInfo, Code: "a.info"})
	q.Push(proto.Event{Severity: proto.SeverityCritical, Code: "gpu.ecc_error"})
	q.Push(proto.Event{Severity: proto.SeverityInfo, Code: "b.info"})

	// Full. This push must evict an INFO, not the CRITICAL.
	q.Push(proto.Event{Severity: proto.SeverityWarn, Code: "c.warn"})

	got := q.Drain(10)
	var sawCritical bool
	for _, e := range got {
		if e.Code == "gpu.ecc_error" {
			sawCritical = true
		}
	}
	if !sawCritical {
		t.Fatalf("the CRITICAL event must survive eviction, got %+v", got)
	}
	if q.Dropped() != 1 {
		t.Errorf("dropped = %d, want 1", q.Dropped())
	}
}

func TestEventQueueIsFIFO(t *testing.T) {
	q := NewEventQueue(10)
	for _, code := range []string{"a", "b", "c"} {
		q.Push(proto.Event{Code: code, Severity: proto.SeverityWarn})
	}
	got := q.Drain(2)
	if len(got) != 2 || got[0].Code != "a" || got[1].Code != "b" {
		t.Fatalf("got %+v", got)
	}
	if q.Len() != 1 {
		t.Fatalf("Len = %d, want 1", q.Len())
	}
}

func TestAgentEmitsStartAndStopEvents(t *testing.T) {
	a, _ := testAgent(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	<-done

	var started, stopping bool
	for _, e := range a.Events().Drain(100) {
		switch e.Code {
		case proto.CodeAgentStarted:
			started = true
		case proto.CodeAgentStopping:
			stopping = true
		}
	}
	if !started || !stopping {
		t.Fatalf("expected start and stop events, got started=%v stopping=%v", started, stopping)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
