// Package collect gathers host telemetry.
//
// The organising constraint is that sampling must never block. A node is a
// machine in someone's house with a consumer SSD that may be failing, an NVIDIA
// driver that may be mid-upgrade, and a USB disk enclosure that may have
// stopped answering. Any of those can make a read hang for tens of seconds.
//
// So every collector runs behind a timeout, in parallel, and a collector that
// fails or times out is abandoned: its failure is recorded on the sample and
// emitted as an event, and the rest of the sample ships anyway. Dropping a
// whole sample because one collector wedged would turn a minor fault into a
// telemetry gap, which is the one thing the controller cannot distinguish from
// a node being unplugged.
package collect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	proto "github.com/meterhome/meternode-proto"
)

// Collector produces one part of a Sample.
//
// Platform-specific collection sits behind this interface. The agent targets
// linux/amd64 only — we own the hardware and image it ourselves, so there is no
// Windows/WSL2 tax to pay — but keeping collection behind an interface means a
// future bring-your-own-hardware tier does not need a rewrite.
type Collector interface {
	// Name is stable and appears in Sample.CollectorErrors and in
	// agent.collector_failed events. Alert rules key off it.
	Name() string

	// Collect fills in its part of the sample. It must honour ctx: the
	// framework will move on regardless, and a collector that ignores
	// cancellation leaks a goroutine on every sample.
	Collect(ctx context.Context, s *proto.Sample) error
}

// Registry runs a set of collectors under a shared timeout.
type Registry struct {
	mu         sync.RWMutex
	collectors []Collector
	timeout    time.Duration

	// consecutiveFailures tracks each collector's run of failures, so a
	// collector that is permanently broken (a driver removed, a disk gone)
	// reports once and then stays quiet rather than emitting an event every
	// ten seconds forever. That would be a self-inflicted alert storm and,
	// on a metered link, a self-inflicted bandwidth problem.
	consecutiveFailures map[string]int
}

// NewRegistry builds a registry.
func NewRegistry(timeout time.Duration) *Registry {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Registry{timeout: timeout, consecutiveFailures: map[string]int{}}
}

// Register adds a collector. Not safe to call concurrently with Collect, and
// not meant to be: collectors are registered at startup.
func (r *Registry) Register(c ...Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors = append(r.collectors, c...)
}

// Names lists the registered collectors, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.collectors))
	for _, c := range r.collectors {
		out = append(out, c.Name())
	}
	sort.Strings(out)
	return out
}

// Failure is one collector's failure in one sample.
type Failure struct {
	Collector string
	Err       error
	// Consecutive is how many samples in a row this collector has failed.
	// The caller emits an event on the first failure and on recovery, not on
	// every sample in between.
	Consecutive int
	TimedOut    bool
}

// Result is one sampling pass.
type Result struct {
	Sample   proto.Sample
	Failures []Failure
	// Recovered names collectors that failed last time and worked this time.
	Recovered []string
	Duration  time.Duration
}

// ErrCollectorTimeout marks a collector abandoned rather than failed.
var ErrCollectorTimeout = errors.New("collect: collector timed out")

// Collect runs every collector in parallel under the registry's timeout and
// returns a sample plus whatever failed.
//
// It always returns a usable sample. There is no error return, because there is
// no failure mode here that should stop the agent sampling — that is the whole
// point of the package.
func (r *Registry) Collect(ctx context.Context, now time.Time) Result {
	r.mu.RLock()
	collectors := make([]Collector, len(r.collectors))
	copy(collectors, r.collectors)
	timeout := r.timeout
	r.mu.RUnlock()

	start := time.Now()
	res := Result{Sample: proto.Sample{T: now.UnixMilli()}}

	// Each collector writes into its own sample and the results are merged,
	// so two collectors touching the same field cannot race. The alternative —
	// a mutex around one shared sample — would serialise the slow collectors
	// we specifically want running in parallel.
	type outcome struct {
		name    string
		sample  proto.Sample
		err     error
		timeout bool
	}
	outcomes := make([]outcome, len(collectors))

	var wg sync.WaitGroup
	for i, c := range collectors {
		wg.Add(1)
		go func(i int, c Collector) {
			defer wg.Done()

			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var s proto.Sample
			done := make(chan error, 1)
			go func() {
				// The recover MUST be on the goroutine that actually calls
				// Collect. A panic on this inner goroutine is not recoverable
				// from the outer one, and an unrecovered panic in a disk
				// parser takes down an agent on a machine nobody can reach.
				defer func() {
					if p := recover(); p != nil {
						done <- fmt.Errorf("collect: %s panicked: %v", c.Name(), p)
					}
				}()
				done <- c.Collect(cctx, &s)
			}()

			select {
			case err := <-done:
				outcomes[i] = outcome{name: c.Name(), sample: s, err: err}
			case <-cctx.Done():
				// Abandoned. The collector's goroutine may still be blocked in
				// a syscall — that is exactly the case this exists for, and it
				// will exit when the syscall returns. What matters is that the
				// agent does not wait for it.
				outcomes[i] = outcome{
					name:    c.Name(),
					err:     fmt.Errorf("%w after %s", ErrCollectorTimeout, timeout),
					timeout: true,
				}
			}
		}(i, c)
	}
	wg.Wait()

	r.mu.Lock()
	for _, o := range outcomes {
		if o.err != nil {
			r.consecutiveFailures[o.name]++
			res.Failures = append(res.Failures, Failure{
				Collector:   o.name,
				Err:         o.err,
				Consecutive: r.consecutiveFailures[o.name],
				TimedOut:    o.timeout,
			})
			res.Sample.CollectorErrors = append(res.Sample.CollectorErrors, o.name)
			continue
		}
		if r.consecutiveFailures[o.name] > 0 {
			res.Recovered = append(res.Recovered, o.name)
			delete(r.consecutiveFailures, o.name)
		}
		merge(&res.Sample, &o.sample)
	}
	r.mu.Unlock()

	sort.Strings(res.Sample.CollectorErrors)
	sort.Strings(res.Recovered)
	res.Duration = time.Since(start)
	return res
}

// merge folds one collector's partial sample into the accumulated one.
//
// Only non-nil groups are copied, which is what preserves the "absent is not
// zero" property: a GPU collector that failed leaves Sample.GPUs nil, and the
// controller can tell that from a GPU sitting idle at 0 W.
func merge(dst, src *proto.Sample) {
	if src.CPU != nil {
		dst.CPU = src.CPU
	}
	if src.Mem != nil {
		dst.Mem = src.Mem
	}
	if src.Net != nil {
		dst.Net = src.Net
	}
	if src.Host != nil {
		dst.Host = src.Host
	}
	if src.Disk != nil {
		dst.Disk = append(dst.Disk, src.Disk...)
	}
	if src.GPUs != nil {
		dst.GPUs = append(dst.GPUs, src.GPUs...)
	}
	if src.Containers != nil {
		dst.Containers = append(dst.Containers, src.Containers...)
	}
}

// Events converts a Result's failures and recoveries into events to ship.
//
// The reporting rule is deliberate: an event on the FIRST failure of a run, and
// an event on recovery, and nothing in between. A permanently broken collector
// — a driver removed, a disk pulled — would otherwise emit an event every ten
// seconds forever, which is both an alert storm and, on a metered residential
// link, a bandwidth problem we inflicted on ourselves.
func (r *Result) Events(now time.Time) []proto.Event {
	var out []proto.Event
	for _, f := range r.Failures {
		if f.Consecutive != 1 {
			continue
		}
		severity := proto.SeverityWarn
		if f.TimedOut {
			// A timeout means something on the host is wedged, which is worse
			// than a collector that returned a clean error.
			severity = proto.SeverityError
		}
		out = append(out, proto.Event{
			T:        now.UnixMilli(),
			Severity: severity,
			Code:     proto.CodeCollectorFailed,
			Message:  fmt.Sprintf("collector %s failed: %v", f.Collector, f.Err),
			Detail: map[string]string{
				"collector": f.Collector,
				"timed_out": fmt.Sprint(f.TimedOut),
			},
		})
	}
	for _, name := range r.Recovered {
		out = append(out, proto.Event{
			T:        now.UnixMilli(),
			Severity: proto.SeverityInfo,
			Code:     proto.CodeCollectorFailed,
			Message:  fmt.Sprintf("collector %s recovered", name),
			Detail:   map[string]string{"collector": name, "recovered": "true"},
		})
	}
	return out
}
