package collect

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	proto "github.com/meterhome/meternode-proto"
	"github.com/shirou/gopsutil/v3/host"

	"github.com/meterhome/meternode-agent/internal/version"
)

// HostCollector reports process and OS identity.
type HostCollector struct {
	// startedAt is the agent process start, not host boot. Together with the
	// boot id it separates "the homeowner power-cycled the machine" from "the
	// agent crashed and systemd restarted it" — very different problems that
	// must not share an alert.
	startedAt time.Time

	// clockSkew is the agent's current estimate of its own clock error against
	// the controller, set by the transport on every round trip. Residential
	// machines lose their RTC across power cuts and come back minutes off.
	clockSkew func() time.Duration
}

// NewHostCollector builds the collector. clockSkew may be nil before the
// transport exists (M3), in which case skew reports as zero.
func NewHostCollector(startedAt time.Time, clockSkew func() time.Duration) *HostCollector {
	return &HostCollector{startedAt: startedAt, clockSkew: clockSkew}
}

func (h *HostCollector) Name() string { return "host" }

func (h *HostCollector) Collect(ctx context.Context, s *proto.Sample) error {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		// Fall back to what can be read without gopsutil rather than failing
		// the collector outright. The agent version and the kernel are the two
		// fields the controller most needs, and both are cheap.
		s.Host = &proto.Host{
			AgentVersion: version.Version,
			OS:           runtime.GOOS,
			Kernel:       readTrimmed("/proc/sys/kernel/osrelease"),
			BootID:       readTrimmed("/proc/sys/kernel/random/boot_id"),
			ClockSkewMS:  h.skewMS(),
		}
		return err
	}

	s.Host = &proto.Host{
		UptimeS:      info.Uptime,
		AgentVersion: version.Version,
		Kernel:       info.KernelVersion,
		OS:           strings.TrimSpace(info.Platform + " " + info.PlatformVersion),
		BootID:       info.HostID,
		ClockSkewMS:  h.skewMS(),
	}
	// gopsutil's HostID is the machine-id, which is stable across reboots.
	// BootID must change on every boot for the crash-loop distinction to work,
	// so prefer the kernel's value when it is readable.
	if bootID := readTrimmed("/proc/sys/kernel/random/boot_id"); bootID != "" {
		s.Host.BootID = bootID
	}
	return nil
}

// AgentUptime is the agent process uptime, used by the heartbeat.
func (h *HostCollector) AgentUptime() time.Duration { return time.Since(h.startedAt) }

func (h *HostCollector) skewMS() int32 {
	if h.clockSkew == nil {
		return 0
	}
	return int32(h.clockSkew().Milliseconds())
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
