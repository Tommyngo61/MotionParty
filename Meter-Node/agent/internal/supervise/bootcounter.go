package supervise

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BootRecord is the persisted restart history.
//
// The agent has to be able to tell three things apart, and from the
// controller's side they can look identical:
//
//   - The homeowner power-cycled the machine. Routine.
//   - The agent exited cleanly and systemd restarted it. Usually an update.
//   - The agent is crash-looping. A real fault, and the one that must not be
//     silent — a node that crashes every 30 seconds heartbeats often enough to
//     look alive.
type BootRecord struct {
	BootCount  uint64    `json:"boot_count"`
	CrashCount uint64    `json:"crash_count"`
	LastStart  time.Time `json:"last_start"`
	// CleanShutdown is written on the way out. If it is false at the next
	// start, the previous run died without getting the chance — a panic, an
	// OOM kill, a power cut.
	CleanShutdown bool `json:"clean_shutdown"`
	// ConsecutiveCrashes resets on a run that lasts longer than
	// CrashLoopMinUptime.
	ConsecutiveCrashes uint32 `json:"consecutive_crashes"`
}

// CrashLoopThreshold is how many consecutive crashes count as a loop.
const CrashLoopThreshold = 3

// CrashLoopMinUptime is how long a run must last to count as "not a crash
// loop". Shorter than a flush interval: an agent that never survives long
// enough to ship a single batch is not working, whatever its exit status said.
const CrashLoopMinUptime = 90 * time.Second

// BootCounter persists the record.
type BootCounter struct {
	path   string
	record BootRecord
	start  time.Time
}

// LoadBootCounter reads the record and increments the boot count.
//
// A corrupt or missing file starts fresh rather than failing. Losing the crash
// history is a much smaller problem than an agent that refuses to start because
// it cannot parse its own bookkeeping.
func LoadBootCounter(path string) (*BootCounter, error) {
	bc := &BootCounter{path: path, start: time.Now()}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &bc.record)
	}

	if !bc.record.CleanShutdown && bc.record.BootCount > 0 {
		bc.record.CrashCount++
		bc.record.ConsecutiveCrashes++
	}
	bc.record.BootCount++
	bc.record.LastStart = bc.start
	bc.record.CleanShutdown = false

	if err := bc.save(); err != nil {
		return bc, err
	}
	return bc, nil
}

// Record returns a copy of the current record.
func (b *BootCounter) Record() BootRecord { return b.record }

// InCrashLoop reports whether the agent has crashed repeatedly.
//
// The controller turns this into a CRITICAL event on the next connection. It is
// the signal that separates "this node is fine" from "this node has been
// restarting every thirty seconds for an hour and its heartbeats have been
// lying to you".
func (b *BootCounter) InCrashLoop() bool {
	return b.record.ConsecutiveCrashes >= CrashLoopThreshold
}

// MarkStable clears the consecutive-crash count once the agent has run long
// enough to prove it is not looping. Called from the main loop, not at start.
func (b *BootCounter) MarkStable() error {
	if time.Since(b.start) < CrashLoopMinUptime || b.record.ConsecutiveCrashes == 0 {
		return nil
	}
	b.record.ConsecutiveCrashes = 0
	return b.save()
}

// MarkCleanShutdown records that this run ended deliberately.
func (b *BootCounter) MarkCleanShutdown() error {
	b.record.CleanShutdown = true
	return b.save()
}

// save writes the record atomically.
//
// Write-to-temp-and-rename, because the thing this file exists to survive is
// exactly the thing that corrupts a partial write: a homeowner pulling the
// plug mid-write.
func (b *BootCounter) save() error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o750); err != nil {
		return fmt.Errorf("supervise: create state directory: %w", err)
	}
	data, err := json.Marshal(b.record)
	if err != nil {
		return err
	}

	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return fmt.Errorf("supervise: write boot counter: %w", err)
	}
	if err := os.Rename(tmp, b.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("supervise: replace boot counter: %w", err)
	}
	return nil
}
