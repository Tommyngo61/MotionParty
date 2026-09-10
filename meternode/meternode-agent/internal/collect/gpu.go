package collect

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	proto "github.com/meterhome/meternode-proto"
)

// GPUSource reads GPU telemetry from somewhere.
//
// There are two implementations by design. NVML (M2) is the fast path: an
// in-process library call, cheap enough to run every 10 s. `nvidia-smi` is the
// fallback: a fork and a CSV parse, an order of magnitude more expensive, but
// it works when the NVML bindings cannot load.
//
// The fallback is not paranoia. A node's driver gets upgraded — sometimes by
// our own update, sometimes by unattended-upgrades on the base image — and
// during that window NVML's shared library and the kernel module disagree about
// version. The GPU is fine; only the library is briefly unusable. An agent that
// treated that as a fatal error would take a healthy node out of the fleet for
// the length of a driver upgrade.
type GPUSource interface {
	Name() string
	// Available reports whether this source can be used right now. It is
	// re-checked periodically, because "right now" changes.
	Available(ctx context.Context) bool
	Read(ctx context.Context) ([]proto.GPU, error)
}

// ErrNoDriver means no NVIDIA driver is present or usable.
var ErrNoDriver = errors.New("collect: no usable NVIDIA driver")

// GPUCollector reads GPU telemetry through the best available source.
type GPUCollector struct {
	mu      sync.Mutex
	sources []GPUSource
	active  GPUSource

	// lastProbe throttles re-checking sources. Probing is a fork in the
	// fallback case, and doing it every 10 s on a node whose driver is
	// genuinely gone would be a self-inflicted load problem.
	lastProbe     time.Time
	probeInterval time.Duration
}

// NewGPUCollector builds the collector over an ordered preference list.
//
// Phase 1 ships only the nvidia-smi source; the NVML source lands in M2 and
// goes at the front of this list. The ordering, the probe throttling, and the
// degraded-path handling are all here now so that change is one line.
func NewGPUCollector(sources ...GPUSource) *GPUCollector {
	if len(sources) == 0 {
		sources = []GPUSource{NewSMISource("")}
	}
	return &GPUCollector{sources: sources, probeInterval: time.Minute}
}

func (g *GPUCollector) Name() string { return "gpu" }

func (g *GPUCollector) Collect(ctx context.Context, s *proto.Sample) error {
	src := g.pick(ctx)
	if src == nil {
		// Sample.GPUs stays nil. That is the whole point of ADR 0003: the
		// controller can tell "no GPU data" from "a GPU idling at 0 W", and
		// only the first means the driver is missing.
		return fmt.Errorf("%w (tried %s)", ErrNoDriver, strings.Join(g.sourceNames(), ", "))
	}

	gpus, err := src.Read(ctx)
	if err != nil {
		// Drop the cached source so the next sample re-probes. A driver that
		// vanished mid-run should promote the fallback immediately, not on the
		// next probe interval.
		g.mu.Lock()
		g.active = nil
		g.lastProbe = time.Time{}
		g.mu.Unlock()
		return err
	}
	if len(gpus) == 0 {
		return fmt.Errorf("collect: %s reported no GPUs", src.Name())
	}

	s.GPUs = gpus
	return nil
}

// ActiveSource names the source currently in use, for `meternodectl status`.
func (g *GPUCollector) ActiveSource() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active == nil {
		return "none"
	}
	return g.active.Name()
}

func (g *GPUCollector) pick(ctx context.Context) GPUSource {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.active != nil && time.Since(g.lastProbe) < g.probeInterval {
		return g.active
	}
	for _, src := range g.sources {
		if src.Available(ctx) {
			g.active, g.lastProbe = src, time.Now()
			return src
		}
	}
	g.active, g.lastProbe = nil, time.Now()
	return nil
}

func (g *GPUCollector) sourceNames() []string {
	out := make([]string, 0, len(g.sources))
	for _, s := range g.sources {
		out = append(out, s.Name())
	}
	return out
}

// --- nvidia-smi source ------------------------------------------------------

// SMISource reads GPU telemetry by running nvidia-smi.
type SMISource struct {
	binary string
}

// NewSMISource builds the source. An empty binary means look up nvidia-smi on
// PATH.
func NewSMISource(binary string) *SMISource {
	if binary == "" {
		binary = "nvidia-smi"
	}
	return &SMISource{binary: binary}
}

func (s *SMISource) Name() string { return "nvidia-smi" }

func (s *SMISource) Available(ctx context.Context) bool {
	path, err := exec.LookPath(s.binary)
	if err != nil {
		return false
	}
	// Present on PATH is not the same as working: during a driver upgrade the
	// binary exists but reports a library/kernel-module mismatch. Run the
	// cheapest possible query to find out which situation this is.
	cmd := exec.CommandContext(ctx, path, "--query-gpu=uuid", "--format=csv,noheader")
	return cmd.Run() == nil
}

// smiFields is the query, in the order the parser expects. Changing the order
// here without changing the parser silently mislabels every metric, so the two
// are kept adjacent.
var smiFields = []string{
	"index", "uuid", "name", "utilization.gpu", "memory.used", "memory.total",
	"temperature.gpu", "power.draw", "power.limit", "fan.speed", "clocks.sm",
	"pcie.link.gen.current", "pcie.link.width.current",
	"ecc.errors.uncorrected.volatile.total", "clocks_throttle_reasons.active",
	"driver_version",
}

func (s *SMISource) Read(ctx context.Context) ([]proto.GPU, error) {
	cmd := exec.CommandContext(ctx, s.binary,
		"--query-gpu="+strings.Join(smiFields, ","),
		"--format=csv,noheader,nounits")

	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("collect: nvidia-smi failed: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("collect: run nvidia-smi: %w", err)
	}
	return parseSMI(string(out))
}

// parseSMI turns nvidia-smi's CSV into samples.
//
// Every field is individually tolerant of "[N/A]" and "[Not Supported]", which
// nvidia-smi emits per-field on cards that do not expose a sensor — a
// consumer-derived card with no fan reading, ECC disabled, and so on. One
// unreadable field must not lose the whole GPU: temperature and power are the
// signals that matter most and they are almost always present.
func parseSMI(out string) ([]proto.GPU, error) {
	var gpus []proto.GPU

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < len(smiFields) {
			return nil, fmt.Errorf("collect: nvidia-smi returned %d fields, expected %d", len(f), len(smiFields))
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}

		g := proto.GPU{
			Index:           uint8(parseUint(f[0])),
			UUID:            f[1],
			Name:            f[2],
			UtilPct:         parseFloat(f[3]),
			MemUsedMB:       uint32(parseUint(f[4])),
			MemTotalMB:      uint32(parseUint(f[5])),
			TempC:           parseFloat(f[6]),
			PowerW:          parseFloat(f[7]),
			PowerLimitW:     parseFloat(f[8]),
			FanPct:          parseFloat(f[9]),
			SMClockMHz:      uint32(parseUint(f[10])),
			PCIeGen:         uint8(parseUint(f[11])),
			PCIeWidth:       uint8(parseUint(f[12])),
			ECCErrors:       parseUint(f[13]),
			ThrottleReasons: parseThrottleReasons(f[14]),
			DriverVersion:   f[15],
		}
		if g.UUID == "" {
			return nil, fmt.Errorf("collect: nvidia-smi returned a GPU with no UUID")
		}
		gpus = append(gpus, g)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("collect: read nvidia-smi output: %w", err)
	}
	return gpus, nil
}

// notAvailable are the placeholders nvidia-smi substitutes for a field the card
// or driver does not expose.
func notAvailable(v string) bool {
	switch strings.ToLower(v) {
	case "", "[n/a]", "n/a", "[not supported]", "not supported", "[unknown error]":
		return true
	}
	return false
}

func parseFloat(v string) float32 {
	if notAvailable(v) {
		return 0
	}
	f, err := strconv.ParseFloat(v, 32)
	if err != nil {
		return 0
	}
	return float32(f)
}

func parseUint(v string) uint64 {
	if notAvailable(v) {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseThrottleReasons normalises nvidia-smi's throttle string.
//
// A GPU in a homeowner's cabinet with the vents against a wall throttles for
// reasons that matter (thermal, power) and reasons that do not ("gpu idle"),
// and the health scorer must not treat an idle card as a degraded one.
func parseThrottleReasons(v string) []string {
	if notAvailable(v) || v == "0x0000000000000000" {
		return nil
	}
	var out []string
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ',' }) {
		part = strings.ToLower(strings.TrimSpace(part))
		part = strings.ReplaceAll(part, " ", "_")
		switch part {
		case "", "gpu_idle", "not_active", "active":
			continue
		}
		out = append(out, part)
	}
	return out
}
