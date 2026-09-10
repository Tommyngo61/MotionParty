// Package agent is the supervisor loop: it owns the sampler, the buffer, the
// local diagnostic socket, and the agent's own health.
//
// Phase 1 (M1) collects and buffers. The transport (M3) drains the buffer; the
// seams it plugs into — the ControlPlaneCounter, the clock-skew source, the
// event sink — are already here, so that milestone adds a package rather than
// rewriting this one.
package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"

	"github.com/MeterHome/Meter-Node/agent/internal/buffer"
	"github.com/MeterHome/Meter-Node/agent/internal/collect"
	"github.com/MeterHome/Meter-Node/agent/internal/config"
	"github.com/MeterHome/Meter-Node/agent/internal/localapi"
	"github.com/MeterHome/Meter-Node/agent/internal/supervise"
	"github.com/MeterHome/Meter-Node/agent/internal/version"
)

// Agent is the supervisor.
type Agent struct {
	log  *slog.Logger
	boot *supervise.BootCounter

	mu       sync.RWMutex
	cfg      *config.Config
	registry *collect.Registry

	buf    *buffer.Ring
	events *EventQueue

	host *collect.HostCollector
	gpu  *collect.GPUCollector
	net  *collect.NetCollector

	startedAt time.Time

	// Live state read by the local API. Behind the same mutex as cfg because
	// `meternodectl status` and a SIGHUP reload can land at the same moment.
	lastSample     *proto.Sample
	lastSampleAt   time.Time
	lastSampleTook time.Duration
	samplesTaken   uint64
	failing        []string

	// healthy is what the systemd watchdog consults. It goes false when
	// sampling stops making progress, which is the wedge a plain liveness
	// check cannot see — the process is very much alive.
	lastProgress time.Time

	// reloaded wakes the sampling loop so a SIGHUP that changed the sample
	// interval takes effect now rather than at the next tick of the OLD
	// interval — which, if an operator had just lengthened it to conserve
	// bandwidth, could be minutes away.
	reloaded chan struct{}
}

// Options configure an Agent.
type Options struct {
	Config *config.Config
	Logger *slog.Logger
	Boot   *supervise.BootCounter
}

// New builds an Agent with its collectors registered.
func New(o Options) *Agent {
	now := time.Now()
	a := &Agent{
		log:          o.Logger,
		boot:         o.Boot,
		cfg:          o.Config,
		startedAt:    now,
		lastProgress: now,
		events:       NewEventQueue(256),
		reloaded:     make(chan struct{}, 1),
	}

	sampleSeconds := int(o.Config.Collect.SampleInterval.Seconds())
	a.buf = buffer.NewRing(buffer.RingCapacityFor(sampleSeconds, 6*60*60))

	// clockSkew is nil until the transport exists (M3); the host collector
	// reports zero skew until then rather than guessing.
	a.host = collect.NewHostCollector(now, nil)
	a.gpu = collect.NewGPUCollector()
	a.net = collect.NewNetCollector(o.Config.Collect.NetInterface, nil)

	a.registry = collect.NewRegistry(o.Config.Collect.CollectorTimeout)
	a.registry.Register(
		a.host,
		collect.NewCPUCollector(),
		collect.NewMemCollector(),
		collect.NewDiskCollector(o.Config.Collect.DiskMounts),
		a.net,
		a.gpu,
	)
	return a
}

// Run drives the sampling loop until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	a.log.Info("agent started",
		"version", version.String(),
		"collectors", a.registry.Names(),
		"sample_interval", a.interval(),
		"flush_interval", a.cfg.Collect.FlushInterval,
		"buffer_capacity", a.buf.Cap())

	a.events.Push(proto.Event{
		T: time.Now().UnixMilli(), Severity: proto.SeverityInfo,
		Code: proto.CodeAgentStarted, Message: "agent started",
		Detail: map[string]string{
			"version":    version.Version,
			"boot_count": itoa(a.boot.Record().BootCount),
		},
	})

	// A crash loop must surface as a CRITICAL event rather than as silence. A
	// node that restarts every thirty seconds still heartbeats often enough to
	// look alive from the controller's side.
	if a.boot.InCrashLoop() {
		rec := a.boot.Record()
		a.log.Error("agent is in a crash loop",
			"consecutive_crashes", rec.ConsecutiveCrashes, "total_crashes", rec.CrashCount)
		a.events.Push(proto.Event{
			T: time.Now().UnixMilli(), Severity: proto.SeverityCritical,
			Code:    proto.CodeAgentCrashLoop,
			Message: "agent has restarted repeatedly without completing a stable run",
			Detail: map[string]string{
				"consecutive_crashes": itoa(uint64(rec.ConsecutiveCrashes)),
				"total_crashes":       itoa(rec.CrashCount),
			},
		})
	}

	// Sample immediately rather than after one interval. A tech who has just
	// started the agent and runs `meternodectl status` should see real data,
	// not an empty struct.
	a.sample(ctx)

	ticker := time.NewTicker(a.interval())
	defer ticker.Stop()
	stability := time.NewTicker(30 * time.Second)
	defer stability.Stop()

	for {
		select {
		case <-ctx.Done():
			a.log.Info("agent stopping", "samples_taken", a.samplesTaken, "buffered", a.buf.Len())
			a.events.Push(proto.Event{
				T: time.Now().UnixMilli(), Severity: proto.SeverityInfo,
				Code: proto.CodeAgentStopping, Message: "agent stopping",
			})
			return nil

		case <-ticker.C:
			a.sample(ctx)

		case <-a.reloaded:
			ticker.Reset(a.interval())

		case <-stability.C:
			// Once the agent has run long enough to prove it is not looping,
			// clear the counter so a single bad restart does not follow a node
			// around forever.
			if err := a.boot.MarkStable(); err != nil {
				a.log.Warn("could not update the boot counter", "error", err)
			}
		}
	}
}

// sample runs one collection pass and buffers the result.
func (a *Agent) sample(ctx context.Context) {
	a.mu.RLock()
	registry := a.registry
	a.mu.RUnlock()

	res := registry.Collect(ctx, time.Now())

	for _, ev := range res.Events(time.Now()) {
		a.events.Push(ev)
		if ev.Severity >= proto.SeverityError {
			a.log.Error("collector failed", "collector", ev.Detail["collector"], "message", ev.Message)
		} else {
			a.log.Warn("collector state changed", "collector", ev.Detail["collector"], "message", ev.Message)
		}
	}

	a.buf.Push(res.Sample)

	a.mu.Lock()
	sample := res.Sample
	a.lastSample = &sample
	a.lastSampleAt = time.Now()
	a.lastSampleTook = res.Duration
	a.samplesTaken++
	a.lastProgress = time.Now()
	a.failing = a.failing[:0]
	a.failing = append(a.failing, res.Sample.CollectorErrors...)
	a.mu.Unlock()

	// A sampling pass that takes most of its own interval means the host is in
	// trouble — a dying disk, or a GPU node so loaded that our timeouts are
	// firing. Worth a log line, not an event: it is a symptom, and the
	// underlying collector failure is already reported.
	if interval := a.interval(); res.Duration > interval/2 {
		a.log.Warn("sampling is slow",
			"duration", res.Duration, "interval", interval,
			"failing", res.Sample.CollectorErrors)
	}
}

// Reload applies a new configuration on SIGHUP.
//
// Only the fields config.Reloadable permits are applied. Anything touching
// identity or the transport needs a restart, because reconnecting to apply a
// changed controller URL would drop a socket that may have taken minutes of
// backoff to establish on a flaky residential link.
func (a *Agent) Reload(next *config.Config) {
	registry := collect.NewRegistry(next.Collect.CollectorTimeout)
	registry.Register(
		a.host,
		collect.NewCPUCollector(),
		collect.NewMemCollector(),
		collect.NewDiskCollector(next.Collect.DiskMounts),
		a.net,
		a.gpu,
	)

	a.mu.Lock()
	prev := a.cfg
	a.cfg = next
	// The registry is swapped under the same lock as the config, so a
	// concurrent sample or status call sees a consistent pair rather than the
	// new intervals with the old collectors.
	a.registry = registry
	a.mu.Unlock()

	if prev.Collect.NetInterface != next.Collect.NetInterface {
		a.net.ResetInterface()
	}

	// Wake the sampling loop so a changed interval applies now. Non-blocking:
	// a reload that arrives while one is already pending is a no-op, which is
	// correct — the loop will read the latest config either way.
	select {
	case a.reloaded <- struct{}{}:
	default:
	}

	a.log.Info("configuration reloaded",
		"sample_interval", next.Collect.SampleInterval,
		"collector_timeout", next.Collect.CollectorTimeout,
		"log_level", next.Log.Level)

	a.events.Push(proto.Event{
		T: time.Now().UnixMilli(), Severity: proto.SeverityInfo,
		Code: proto.CodeAgentConfigReload, Message: "configuration reloaded on SIGHUP",
	})
}

// interval returns the current sample interval under the lock.
func (a *Agent) interval() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Collect.SampleInterval
}

// Healthy reports whether the agent is making progress.
//
// This is what the systemd watchdog consults, and it is deliberately about
// progress rather than liveness: an agent whose sampling loop has stalled is
// still a running process, still answering its socket, and completely useless.
// Three missed intervals is the threshold — one missed interval is a slow
// collector, three is a stall.
func (a *Agent) Healthy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	deadline := 3 * a.cfg.Collect.SampleInterval
	if deadline < 30*time.Second {
		deadline = 30 * time.Second
	}
	return time.Since(a.lastProgress) < deadline
}

// Events exposes the queue the transport will drain (M3).
func (a *Agent) Events() *EventQueue { return a.events }

// Buffer exposes the sample buffer the transport will drain (M3).
func (a *Agent) Buffer() *buffer.Ring { return a.buf }

// Status renders the local API's view of the agent.
func (a *Agent) Status(enrolled bool, nodeID, siteID string) localapi.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()

	st := localapi.Status{
		AgentVersion:    version.Version,
		StartedAt:       a.startedAt.UTC().Format(time.RFC3339),
		UptimeS:         int64(time.Since(a.startedAt).Seconds()),
		PID:             pid(),
		Enrolled:        enrolled,
		NodeID:          nodeID,
		SiteID:          siteID,
		ControllerURL:   a.cfg.Controller.URL,
		Collectors:      a.registry.Names(),
		GPUSource:       a.gpu.ActiveSource(),
		SampleIntervalS: int(a.cfg.Collect.SampleInterval.Seconds()),
		SamplesTaken:    a.samplesTaken,
		LastSampleMS:    a.lastSampleTook.Milliseconds(),
		Buffered:        a.buf.Len(),
		BufferCapacity:  a.buf.Cap(),
		BufferDropped:   a.buf.Dropped(),
		BudgetMB:        a.cfg.Bandwidth.MonthlyBudgetMB,
		BudgetStatus:    proto.BudgetOK.String(),
		BootCount:       a.boot.Record().BootCount,
		CrashCount:      a.boot.Record().CrashCount,
		Transport:       "none",
	}
	if !a.lastSampleAt.IsZero() {
		st.LastSampleAt = a.lastSampleAt.UTC().Format(time.RFC3339)
	}
	if len(a.failing) > 0 {
		st.FailingCollectors = append([]string(nil), a.failing...)
	}

	// Warnings are what a field tech reads first, so each one says what is
	// wrong in the terms they would use.
	if !enrolled {
		st.Warnings = append(st.Warnings, "This node is not enrolled. Place a one-time token at "+
			a.cfg.Controller.EnrollTokenFile+" and restart the agent.")
		st.EnrollHint = a.cfg.Controller.EnrollTokenFile
	}
	if a.cfg.Controller.URL == "" {
		st.Warnings = append(st.Warnings, "No controller URL is configured. Set controller.url in "+
			config.DefaultPath+".")
	}
	if st.GPUSource == "none" {
		st.Warnings = append(st.Warnings, "No usable NVIDIA driver. GPU telemetry is unavailable; "+
			"check `nvidia-smi` and whether a driver upgrade is in progress.")
	}
	for _, name := range a.failing {
		st.Warnings = append(st.Warnings, "Collector "+name+" is failing.")
	}
	if a.buf.Dropped() > 0 {
		st.Warnings = append(st.Warnings, "The sample buffer has overflowed and dropped data — "+
			"this node has been unable to reach the controller for a long time.")
	}
	if a.boot.InCrashLoop() {
		st.Warnings = append(st.Warnings, "The agent has restarted repeatedly without a stable run. "+
			"Check `journalctl -u meternode-agent`.")
	}
	return st
}

// LastSample returns the most recent sample for `meternodectl sample`.
func (a *Agent) LastSample() any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.lastSample == nil {
		return nil
	}
	cp := *a.lastSample
	return cp
}
