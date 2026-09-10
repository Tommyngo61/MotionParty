package agent

import (
	"os"
	"strconv"
	"sync"

	proto "github.com/MeterHome/Meter-Node/proto"
)

// EventQueue buffers events until the transport can ship them.
//
// Bounded and oldest-first, for the same reason the sample ring is: a node
// offline for a week must not grow its queue until the kernel kills it. Events
// are more precious than samples — an ECC error matters more than a
// utilisation reading from the same second — but not precious enough to be
// worth an OOM on a machine nobody can reach.
type EventQueue struct {
	mu      sync.Mutex
	items   []proto.Event
	max     int
	dropped uint64
}

// NewEventQueue builds a queue.
func NewEventQueue(max int) *EventQueue {
	if max <= 0 {
		max = 256
	}
	return &EventQueue{max: max}
}

// Push adds an event, evicting the oldest if the queue is full.
func (q *EventQueue) Push(e proto.Event) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) >= q.max {
		// Evict the oldest INFO event in preference to the oldest event
		// overall. An hour of "collector recovered" notices must not push out
		// the one CRITICAL that explains why the node is being replaced.
		if idx := q.oldestLowSeverity(); idx >= 0 {
			q.items = append(q.items[:idx], q.items[idx+1:]...)
		} else {
			q.items = q.items[1:]
		}
		q.dropped++
	}
	q.items = append(q.items, e)
}

func (q *EventQueue) oldestLowSeverity() int {
	for i, e := range q.items {
		if e.Severity <= proto.SeverityInfo {
			return i
		}
	}
	return -1
}

// Drain removes and returns up to n events, oldest first.
func (q *EventQueue) Drain(n int) []proto.Event {
	q.mu.Lock()
	defer q.mu.Unlock()

	if n <= 0 || len(q.items) == 0 {
		return nil
	}
	if n > len(q.items) {
		n = len(q.items)
	}
	out := make([]proto.Event, n)
	copy(out, q.items[:n])
	q.items = append(q.items[:0], q.items[n:]...)
	return out
}

// Len is the queued event count.
func (q *EventQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Dropped is the running count of evicted events.
func (q *EventQueue) Dropped() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}

func itoa(v uint64) string { return strconv.FormatUint(v, 10) }

func pid() int { return os.Getpid() }
