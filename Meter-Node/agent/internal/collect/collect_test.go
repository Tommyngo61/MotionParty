package collect

import (
	"context"
	"errors"
	"testing"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
)

// stubCollector is a collector with scriptable behaviour.
type stubCollector struct {
	name  string
	err   error
	delay time.Duration
	fill  func(*proto.Sample)
	calls int
}

func (s *stubCollector) Name() string { return s.name }

func (s *stubCollector) Collect(ctx context.Context, sample *proto.Sample) error {
	s.calls++
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.err != nil {
		return s.err
	}
	if s.fill != nil {
		s.fill(sample)
	}
	return nil
}

// TestWedgedCollectorDoesNotStallSampling is the property the whole package
// exists for. A statfs against a dying consumer SSD can block for tens of
// seconds in uninterruptible sleep; the agent must move on without it.
func TestWedgedCollectorDoesNotStallSampling(t *testing.T) {
	wedged := &stubCollector{name: "wedged", delay: 10 * time.Second}
	healthy := &stubCollector{name: "cpu", fill: func(s *proto.Sample) {
		s.CPU = &proto.CPU{UtilPct: 42}
	}}

	r := NewRegistry(50 * time.Millisecond)
	r.Register(wedged, healthy)

	start := time.Now()
	res := r.Collect(context.Background(), time.Now())
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("sampling waited %s for a wedged collector", elapsed)
	}
	// The healthy collector's data still shipped. Dropping the whole sample
	// because one collector hung would turn a minor fault into a telemetry
	// gap — which the controller cannot tell from an unplugged node.
	if res.Sample.CPU == nil || res.Sample.CPU.UtilPct != 42 {
		t.Fatal("the healthy collector's data must still be in the sample")
	}
	if len(res.Failures) != 1 || res.Failures[0].Collector != "wedged" {
		t.Fatalf("expected exactly the wedged collector to fail, got %+v", res.Failures)
	}
	if !res.Failures[0].TimedOut {
		t.Error("the failure should be marked as a timeout, not a plain error")
	}
	if !errors.Is(res.Failures[0].Err, ErrCollectorTimeout) {
		t.Errorf("failure error = %v, want ErrCollectorTimeout", res.Failures[0].Err)
	}
	if len(res.Sample.CollectorErrors) != 1 || res.Sample.CollectorErrors[0] != "wedged" {
		t.Errorf("the sample must name the failed collector, got %v", res.Sample.CollectorErrors)
	}
}

// TestPanickingCollectorDoesNotKillTheAgent: a nil map in a disk parser is not
// worth a truck roll.
func TestPanickingCollectorDoesNotKillTheAgent(t *testing.T) {
	panicky := &panicCollector{}
	healthy := &stubCollector{name: "mem", fill: func(s *proto.Sample) {
		s.Mem = &proto.Mem{UsedMB: 1024, TotalMB: 65536}
	}}

	r := NewRegistry(time.Second)
	r.Register(panicky, healthy)

	res := r.Collect(context.Background(), time.Now())
	if res.Sample.Mem == nil {
		t.Fatal("the healthy collector must still produce data")
	}
	if len(res.Failures) != 1 || res.Failures[0].Collector != "panicky" {
		t.Fatalf("the panic must be recorded as a failure, got %+v", res.Failures)
	}
}

type panicCollector struct{}

func (p *panicCollector) Name() string { return "panicky" }
func (p *panicCollector) Collect(ctx context.Context, s *proto.Sample) error {
	var m map[string]string
	m["boom"] = "boom" // nil map write
	return nil
}

// TestAbsentCollectorLeavesNil pins ADR 0003 at the agent end: a failed GPU
// collector must leave Sample.GPUs nil, not an empty slice or a zero struct.
func TestAbsentCollectorLeavesNil(t *testing.T) {
	r := NewRegistry(time.Second)
	r.Register(&stubCollector{name: "gpu", err: ErrNoDriver})

	res := r.Collect(context.Background(), time.Now())
	if res.Sample.GPUs != nil {
		t.Fatalf("a failed GPU collector must leave GPUs nil, got %+v", res.Sample.GPUs)
	}
}

// TestFailuresAreReportedOnceNotEveryTenSeconds: a permanently broken collector
// would otherwise emit an event every sample forever, which is both an alert
// storm and, on a metered residential link, a self-inflicted bandwidth problem.
func TestFailuresAreReportedOnceNotEveryTenSeconds(t *testing.T) {
	broken := &stubCollector{name: "gpu", err: ErrNoDriver}
	r := NewRegistry(time.Second)
	r.Register(broken)

	now := time.Now()
	var events int
	for i := 0; i < 10; i++ {
		res := r.Collect(context.Background(), now)
		events += len(res.Events(now))
	}
	if events != 1 {
		t.Fatalf("ten failing samples produced %d events, want 1", events)
	}

	// Recovery IS reported — an operator needs to know the node came back.
	broken.err = nil
	broken.fill = func(s *proto.Sample) { s.GPUs = []proto.GPU{{UUID: "GPU-x"}} }
	res := r.Collect(context.Background(), now)
	evs := res.Events(now)
	if len(evs) != 1 || evs[0].Detail["recovered"] != "true" {
		t.Fatalf("recovery must produce exactly one event, got %+v", evs)
	}

	// And a subsequent failure is reported again, because the run was broken.
	broken.err = ErrNoDriver
	broken.fill = nil
	res = r.Collect(context.Background(), now)
	if len(res.Events(now)) != 1 {
		t.Fatal("a failure after a recovery must be reported again")
	}
}

func TestTimeoutIsMoreSevereThanAPlainError(t *testing.T) {
	now := time.Now()

	plain := NewRegistry(time.Second)
	plain.Register(&stubCollector{name: "a", err: errors.New("nope")})
	plainRes := plain.Collect(context.Background(), now)
	if got := plainRes.Events(now)[0].Severity; got != proto.SeverityWarn {
		t.Errorf("a clean error should be WARN, got %s", got)
	}

	// A timeout means something on the HOST is wedged, which is worse.
	wedged := NewRegistry(20 * time.Millisecond)
	wedged.Register(&stubCollector{name: "b", delay: time.Second})
	wedgedRes := wedged.Collect(context.Background(), now)
	if got := wedgedRes.Events(now)[0].Severity; got != proto.SeverityError {
		t.Errorf("a timeout should be ERROR, got %s", got)
	}
}

func TestCollectorsRunInParallel(t *testing.T) {
	// Three collectors that each take 100ms must finish in ~100ms, not 300ms.
	// Serialising them would mean a slow disk delays the GPU read, and at a
	// 10 s interval that adds up fast.
	r := NewRegistry(2 * time.Second)
	for _, name := range []string{"a", "b", "c"} {
		r.Register(&stubCollector{name: name, delay: 100 * time.Millisecond})
	}

	start := time.Now()
	r.Collect(context.Background(), time.Now())
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("collectors took %s; they are not running in parallel", elapsed)
	}
}

func TestMergeAccumulatesLists(t *testing.T) {
	// Two disk collectors (or a disk and a container collector) must append,
	// not overwrite.
	r := NewRegistry(time.Second)
	r.Register(
		&stubCollector{name: "d1", fill: func(s *proto.Sample) { s.Disk = []proto.Disk{{Mount: "/"}} }},
		&stubCollector{name: "d2", fill: func(s *proto.Sample) { s.Disk = []proto.Disk{{Mount: "/var"}} }},
	)
	res := r.Collect(context.Background(), time.Now())
	if len(res.Sample.Disk) != 2 {
		t.Fatalf("expected both mounts, got %+v", res.Sample.Disk)
	}
}

func TestRegistryNamesAreSorted(t *testing.T) {
	r := NewRegistry(time.Second)
	r.Register(&stubCollector{name: "net"}, &stubCollector{name: "cpu"}, &stubCollector{name: "disk"})
	got := r.Names()
	want := []string{"cpu", "disk", "net"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}
