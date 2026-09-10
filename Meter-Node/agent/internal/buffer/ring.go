// Package buffer holds telemetry that has not been shipped yet.
//
// Phase 1 (M1) is the in-memory ring below. The SQLite WAL-mode ring buffer
// that survives a restart lands in M3 alongside the transport, behind the same
// Buffer interface — a node that loses power mid-outage should not lose the
// samples it had queued, but a node with no transport has nowhere to send them
// either, so the order is deliberate.
package buffer

import (
	"sync"

	proto "github.com/MeterHome/Meter-Node/proto"
)

// Buffer holds samples between collection and upload.
type Buffer interface {
	Push(s proto.Sample)
	// Drain removes and returns up to n samples, oldest first.
	Drain(n int) []proto.Sample
	Len() int
	Cap() int
	// Dropped is the running count of samples evicted because the buffer was
	// full. It is reported to the controller: a node whose buffer is
	// overflowing is one whose upstream cannot keep up with its own telemetry,
	// which is a real condition on a 5 Mbps link.
	Dropped() uint64
}

// Ring is a fixed-capacity, oldest-first-eviction ring buffer.
//
// Bounded, always. The agent runs on a machine we cannot reach in a house we do
// not control, and an unbounded queue during a week-long ISP outage is an OOM
// kill — which turns a network problem into a node that needs a site visit.
// Dropping the oldest sample is the right trade: recent telemetry is what the
// controller needs first on reconnect, and the drop is itself reported.
type Ring struct {
	mu      sync.Mutex
	items   []proto.Sample
	head    int // index of the oldest item
	size    int
	dropped uint64
}

// NewRing builds a ring of the given capacity.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 1
	}
	return &Ring{items: make([]proto.Sample, capacity)}
}

// RingCapacityFor sizes a ring to hold a given duration of samples.
//
// The default — six hours at a 10 s interval, 2160 samples — is chosen against
// the failure it exists for: a homeowner's ISP outage. Most are minutes; a
// six-hour window covers an evening's outage without the memory cost of trying
// to cover a multi-day one, which is what the SQLite buffer (M3) is for.
func RingCapacityFor(sampleIntervalSeconds, coverSeconds int) int {
	if sampleIntervalSeconds <= 0 {
		sampleIntervalSeconds = 10
	}
	if coverSeconds <= 0 {
		coverSeconds = 6 * 60 * 60
	}
	return coverSeconds / sampleIntervalSeconds
}

func (r *Ring) Push(s proto.Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == len(r.items) {
		// Full: overwrite the oldest and advance the head.
		r.items[r.head] = s
		r.head = (r.head + 1) % len(r.items)
		r.dropped++
		return
	}
	r.items[(r.head+r.size)%len(r.items)] = s
	r.size++
}

func (r *Ring) Drain(n int) []proto.Sample {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n <= 0 || r.size == 0 {
		return nil
	}
	if n > r.size {
		n = r.size
	}
	out := make([]proto.Sample, n)
	for i := 0; i < n; i++ {
		idx := (r.head + i) % len(r.items)
		out[i] = r.items[idx]
		// Zero the slot so the drained sample's slices can be collected
		// rather than pinned by the ring until it wraps.
		r.items[idx] = proto.Sample{}
	}
	r.head = (r.head + n) % len(r.items)
	r.size -= n
	return out
}

func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

func (r *Ring) Cap() int { return len(r.items) }

func (r *Ring) Dropped() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}
