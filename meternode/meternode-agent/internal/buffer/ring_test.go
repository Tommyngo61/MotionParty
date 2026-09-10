package buffer

import (
	"sync"
	"testing"

	proto "github.com/meterhome/meternode-proto"
)

func sample(t int64) proto.Sample { return proto.Sample{T: t} }

func TestRingFIFO(t *testing.T) {
	r := NewRing(4)
	for i := int64(1); i <= 3; i++ {
		r.Push(sample(i))
	}
	if r.Len() != 3 {
		t.Fatalf("Len = %d, want 3", r.Len())
	}

	got := r.Drain(2)
	if len(got) != 2 || got[0].T != 1 || got[1].T != 2 {
		t.Fatalf("Drain must return oldest first, got %+v", got)
	}
	if r.Len() != 1 {
		t.Fatalf("Len after drain = %d, want 1", r.Len())
	}
}

// TestRingEvictsOldestWhenFull is the property that keeps a node alive through
// a week-long ISP outage. An unbounded queue would be an OOM kill, which turns
// a network problem into a machine that needs a site visit.
func TestRingEvictsOldestWhenFull(t *testing.T) {
	r := NewRing(3)
	for i := int64(1); i <= 5; i++ {
		r.Push(sample(i))
	}

	if r.Len() != 3 {
		t.Fatalf("Len = %d, want the capacity 3", r.Len())
	}
	if r.Dropped() != 2 {
		t.Fatalf("Dropped = %d, want 2", r.Dropped())
	}

	// The three most recent survive: on reconnect, recent telemetry is what
	// the controller needs first.
	got := r.Drain(10)
	if len(got) != 3 || got[0].T != 3 || got[2].T != 5 {
		t.Fatalf("expected samples 3,4,5, got %+v", got)
	}
}

func TestRingWrapsRepeatedly(t *testing.T) {
	// The index arithmetic has to survive many wraps, not one. This is a
	// process that runs for months.
	r := NewRing(8)
	for i := int64(1); i <= 1000; i++ {
		r.Push(sample(i))
		if i%3 == 0 {
			r.Drain(1)
		}
	}
	got := r.Drain(100)
	for i := 1; i < len(got); i++ {
		if got[i].T <= got[i-1].T {
			t.Fatalf("ordering broke after wrapping: %+v", got)
		}
	}
}

func TestRingDrainEmpty(t *testing.T) {
	r := NewRing(4)
	if got := r.Drain(5); got != nil {
		t.Fatalf("draining an empty ring should return nil, got %+v", got)
	}
	if got := r.Drain(0); got != nil {
		t.Fatalf("draining zero should return nil, got %+v", got)
	}
}

func TestRingZeroCapacityIsUsable(t *testing.T) {
	// A misconfiguration must not panic on a machine we cannot reach.
	r := NewRing(0)
	r.Push(sample(1))
	r.Push(sample(2))
	if r.Cap() != 1 || r.Len() != 1 {
		t.Fatalf("Cap = %d, Len = %d, want 1 and 1", r.Cap(), r.Len())
	}
}

func TestRingIsConcurrencySafe(t *testing.T) {
	// The sampler pushes while the transport drains. Run under -race.
	r := NewRing(64)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := int64(0); i < 5000; i++ {
			r.Push(sample(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			r.Drain(3)
		}
	}()
	wg.Wait()
}

func TestRingCapacityFor(t *testing.T) {
	if got := RingCapacityFor(10, 6*60*60); got != 2160 {
		t.Fatalf("six hours at 10s = %d samples, want 2160", got)
	}
	// Bad input must produce a usable ring rather than a division by zero.
	if got := RingCapacityFor(0, 0); got != 2160 {
		t.Fatalf("defaults = %d, want 2160", got)
	}
}
