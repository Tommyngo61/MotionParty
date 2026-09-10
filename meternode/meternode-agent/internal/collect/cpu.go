package collect

import (
	"context"
	"strings"

	proto "github.com/meterhome/meternode-proto"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
)

// CPUCollector reports processor state.
type CPUCollector struct{}

func NewCPUCollector() *CPUCollector { return &CPUCollector{} }

func (c *CPUCollector) Name() string { return "cpu" }

func (c *CPUCollector) Collect(ctx context.Context, s *proto.Sample) error {
	out := &proto.CPU{}
	s.CPU = out

	// Percent(0, ...) is the utilisation since the LAST call, not a blocking
	// sample. That matters: the blocking form would hold the collector for its
	// whole interval, and a collector that sleeps is indistinguishable from one
	// that is wedged.
	if pcts, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pcts) > 0 {
		out.UtilPct = float32(pcts[0])
	}

	if avg, err := load.AvgWithContext(ctx); err == nil {
		out.Load1 = float32(avg.Load1)
	}

	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		out.FreqMHz = uint32(infos[0].Mhz)
	}

	// Temperature is best-effort. Plenty of consumer boards expose nothing
	// useful, and a node with no thermal sensor is not a node with a fault —
	// so a missing reading leaves the field at zero rather than failing the
	// collector.
	if temps, err := host.SensorsTemperaturesWithContext(ctx); err == nil {
		out.TempC = float32(pickCPUTemp(temps))
	}

	out.Throttled = readThrottled()
	return nil
}

// pickCPUTemp chooses a package temperature from the sensor soup.
//
// Sensor keys vary by platform: k10temp/Tctl on AMD, coretemp package id 0 on
// Intel, acpitz on some boards. Preferring the package sensor and falling back
// to the hottest core keeps this comparable across a mixed fleet, which is what
// a thermal-throttling alert rule needs.
func pickCPUTemp(temps []host.TemperatureStat) float64 {
	var best float64
	for _, t := range temps {
		key := strings.ToLower(t.SensorKey)
		switch {
		case strings.Contains(key, "tctl"), strings.Contains(key, "package"), strings.Contains(key, "tdie"):
			return t.Temperature
		case strings.Contains(key, "core"), strings.Contains(key, "cpu"), strings.Contains(key, "acpitz"):
			if t.Temperature > best {
				best = t.Temperature
			}
		}
	}
	return best
}

// readThrottled reports whether the kernel is currently limiting the CPU.
//
// On the x86 hardware we ship this reads the thermal-throttle counters under
// sysfs. A non-zero count is historical rather than current, so this is a
// conservative signal: it says "this machine has thermally throttled", which in
// a homeowner's cabinet with the vents against a wall is exactly the thing the
// health scorer wants to know about.
func readThrottled() bool {
	const path = "/sys/devices/system/cpu/cpu0/thermal_throttle/core_throttle_count"
	v := readTrimmed(path)
	return v != "" && v != "0"
}
