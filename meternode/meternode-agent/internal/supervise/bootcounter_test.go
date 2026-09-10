package supervise

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBootCounterCountsBoots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-counter")

	for i := uint64(1); i <= 3; i++ {
		bc, err := LoadBootCounter(path)
		if err != nil {
			t.Fatal(err)
		}
		if bc.Record().BootCount != i {
			t.Fatalf("boot %d: count = %d", i, bc.Record().BootCount)
		}
		if err := bc.MarkCleanShutdown(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCrashLoopIsDetected: a node that restarts every thirty seconds still
// heartbeats often enough to look alive from the controller's side. This is the
// signal that separates "fine" from "has been crashing for an hour".
func TestCrashLoopIsDetected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-counter")

	// A first clean run, so there is a record to crash against.
	bc, err := LoadBootCounter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.MarkCleanShutdown(); err != nil {
		t.Fatal(err)
	}

	// Now crash repeatedly. A crash is only VISIBLE on the following start —
	// the run that died could not record anything — so N crashes need N+1
	// starts after the clean one.
	for i := 0; i < CrashLoopThreshold+1; i++ {
		bc, err = LoadBootCounter(path)
		if err != nil {
			t.Fatal(err)
		}
		// No MarkCleanShutdown — this is what a panic or an OOM kill leaves.
	}

	if !bc.InCrashLoop() {
		t.Fatalf("after %d unclean exits the agent must report a crash loop (record: %+v)",
			CrashLoopThreshold, bc.Record())
	}
	if bc.Record().CrashCount < uint64(CrashLoopThreshold) {
		t.Errorf("crash count = %d, want at least %d", bc.Record().CrashCount, CrashLoopThreshold)
	}
}

// TestCleanShutdownIsNotACrash. An update restarts the agent deliberately;
// that must not look like a fault.
func TestCleanShutdownIsNotACrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-counter")
	for i := 0; i < 5; i++ {
		bc, err := LoadBootCounter(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := bc.MarkCleanShutdown(); err != nil {
			t.Fatal(err)
		}
		if bc.InCrashLoop() {
			t.Fatalf("run %d: clean restarts must never read as a crash loop", i)
		}
		if bc.Record().CrashCount != 0 {
			t.Fatalf("run %d: crash count = %d, want 0", i, bc.Record().CrashCount)
		}
	}
}

// TestStableRunClearsTheCrashStreak: a single bad restart must not follow a
// node around forever.
func TestStableRunClearsTheCrashStreak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-counter")

	bc, _ := LoadBootCounter(path)
	_ = bc.MarkCleanShutdown()
	for i := 0; i < CrashLoopThreshold+1; i++ {
		bc, _ = LoadBootCounter(path)
	}
	if !bc.InCrashLoop() {
		t.Fatal("setup: expected a crash loop")
	}

	// Not yet stable: MarkStable is a no-op before CrashLoopMinUptime.
	if err := bc.MarkStable(); err != nil {
		t.Fatal(err)
	}
	if !bc.InCrashLoop() {
		t.Fatal("a run shorter than CrashLoopMinUptime must not clear the streak")
	}

	// Backdate the start so the run counts as stable.
	bc.start = time.Now().Add(-2 * CrashLoopMinUptime)
	if err := bc.MarkStable(); err != nil {
		t.Fatal(err)
	}
	if bc.InCrashLoop() {
		t.Fatal("a stable run must clear the consecutive-crash streak")
	}

	// And it persisted.
	next, _ := LoadBootCounter(path)
	if next.Record().ConsecutiveCrashes > 1 {
		t.Errorf("the cleared streak should have persisted, got %d", next.Record().ConsecutiveCrashes)
	}
}

// TestCorruptCounterStartsFresh: losing the crash history is much less bad than
// an agent that refuses to start because it cannot parse its own bookkeeping.
func TestCorruptCounterStartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-counter")
	if err := os.WriteFile(path, []byte("{not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	bc, err := LoadBootCounter(path)
	if err != nil {
		t.Fatalf("a corrupt counter must not stop the agent: %v", err)
	}
	if bc.Record().BootCount != 1 {
		t.Errorf("boot count = %d, want 1", bc.Record().BootCount)
	}
}

// TestSaveIsAtomic: the thing this file exists to survive is exactly the thing
// that corrupts a partial write — a homeowner pulling the plug.
func TestSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boot-counter")

	bc, err := LoadBootCounter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.MarkCleanShutdown(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("a temp file was left behind: %s", e.Name())
		}
	}
}

func TestNotifierNoOpsWithoutSystemd(t *testing.T) {
	// A developer running the binary by hand, or the OCI dev image, has no
	// NOTIFY_SOCKET. Every call must be a silent no-op rather than an error.
	t.Setenv("NOTIFY_SOCKET", "")
	n := NewNotifier()
	if n.Enabled() {
		t.Fatal("notifier should be disabled without NOTIFY_SOCKET")
	}
	for name, err := range map[string]error{
		"ready":    n.Ready(),
		"stopping": n.Stopping(),
		"status":   n.Status("x"),
		"watchdog": n.Watchdog(),
	} {
		if err != nil {
			t.Errorf("%s returned %v, want nil", name, err)
		}
	}
	if got := n.WatchdogInterval(); got != 0 {
		t.Errorf("watchdog interval = %s, want 0", got)
	}
}

// TestWatchdogIntervalIsHalfTheDeadline: pinging at exactly the deadline means
// any scheduling jitter on a busy GPU node is a spurious restart.
func TestWatchdogIntervalIsHalfTheDeadline(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "/run/systemd/notify")
	t.Setenv("WATCHDOG_USEC", "60000000") // 60s
	t.Setenv("WATCHDOG_PID", "")

	n := NewNotifier()
	if got := n.WatchdogInterval(); got != 30*time.Second {
		t.Fatalf("interval = %s, want 30s", got)
	}

	// A WATCHDOG_PID naming another process means the variable was inherited
	// and systemd is not watching us.
	t.Setenv("WATCHDOG_PID", "999999")
	if got := n.WatchdogInterval(); got != 0 {
		t.Fatalf("interval = %s, want 0 when WATCHDOG_PID names another process", got)
	}
}
