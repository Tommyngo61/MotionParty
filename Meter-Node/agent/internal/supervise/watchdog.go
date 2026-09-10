// Package supervise holds the agent's own resilience machinery: the systemd
// watchdog, the boot counter, and panic recovery.
//
// All of it exists for one reason. A node lives in a house we cannot reach, on
// a machine nobody there administers. An agent that dies quietly is
// indistinguishable, from the controller's side, from a homeowner who unplugged
// the box — and those need very different responses.
package supervise

import (
	"context"
	"net"
	"os"
	"strconv"
	"time"
)

// Notifier speaks systemd's sd_notify protocol.
//
// Implemented directly rather than via a library because it is twelve lines of
// datagram writing and the alternative pulls in a dependency (or CGO) for it.
// It no-ops cleanly when NOTIFY_SOCKET is unset, which is the case in a
// container, in a test, and when a developer runs the binary by hand.
type Notifier struct {
	addr string
}

// NewNotifier reads NOTIFY_SOCKET.
func NewNotifier() *Notifier { return &Notifier{addr: os.Getenv("NOTIFY_SOCKET")} }

// Enabled reports whether systemd is listening.
func (n *Notifier) Enabled() bool { return n.addr != "" }

func (n *Notifier) send(state string) error {
	if n.addr == "" {
		return nil
	}
	addr := n.addr
	// An abstract socket is written as "@/path" in NOTIFY_SOCKET and needs a
	// NUL as its first byte on the wire.
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}

// Ready tells systemd the agent has finished starting. Paired with
// Type=notify in the unit, this is what makes `systemctl start` block until the
// agent is genuinely up rather than merely forked.
func (n *Notifier) Ready() error { return n.send("READY=1") }

// Stopping tells systemd a clean shutdown is in progress.
func (n *Notifier) Stopping() error { return n.send("STOPPING=1") }

// Status sets the one-line status shown by `systemctl status`. It is the first
// thing a field tech sees, so it carries the thing they most need: enrolled or
// not, connected or not.
func (n *Notifier) Status(s string) error { return n.send("STATUS=" + s) }

// Watchdog pings systemd's WatchdogSec timer.
func (n *Notifier) Watchdog() error { return n.send("WATCHDOG=1") }

// WatchdogInterval returns how often to ping, derived from WATCHDOG_USEC.
//
// Half the configured interval, per systemd's own recommendation: pinging at
// exactly the deadline means any scheduling jitter on a busy GPU node is a
// spurious restart, and a spurious restart on a machine in someone's house is
// the worst kind of self-inflicted outage.
func (n *Notifier) WatchdogInterval() time.Duration {
	usec := os.Getenv("WATCHDOG_USEC")
	if usec == "" {
		return 0
	}
	v, err := strconv.ParseInt(usec, 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	// WATCHDOG_PID guards against inheriting the variable into a child that
	// systemd is not watching.
	if pidStr := os.Getenv("WATCHDOG_PID"); pidStr != "" {
		if pid, err := strconv.Atoi(pidStr); err == nil && pid != os.Getpid() {
			return 0
		}
	}
	return time.Duration(v) * time.Microsecond / 2
}

// RunWatchdog pings systemd until ctx is cancelled.
//
// healthy is consulted before each ping. Returning false stops the pings, which
// lets systemd's WatchdogSec restart an agent that is running but wedged — the
// case a plain liveness check cannot catch, because the process is very much
// alive.
func (n *Notifier) RunWatchdog(ctx context.Context, healthy func() bool) {
	interval := n.WatchdogInterval()
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if healthy != nil && !healthy() {
				// Deliberately silent: systemd will act, and logging on every
				// tick during a wedge would fill the journal.
				continue
			}
			_ = n.Watchdog()
		}
	}
}
