package collect

import (
	"context"
	"errors"
	"testing"

	proto "github.com/meterhome/meternode-proto"
)

// A real nvidia-smi line from an RTX PRO node under load.
const smiHealthy = `0, GPU-1a2b3c4d-5e6f-7081-92a3-b4c5d6e7f809, NVIDIA RTX PRO 6000 Blackwell, 97, 42310, 98304, 71, 412.55, 600.00, 68, 2415, 5, 16, 0, Not Active, 565.57.01`

func TestParseSMIHealthy(t *testing.T) {
	gpus, err := parseSMI(smiHealthy)
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 1 {
		t.Fatalf("got %d GPUs, want 1", len(gpus))
	}
	g := gpus[0]
	if g.UUID != "GPU-1a2b3c4d-5e6f-7081-92a3-b4c5d6e7f809" {
		t.Errorf("uuid = %q", g.UUID)
	}
	if g.UtilPct != 97 || g.TempC != 71 || g.PowerW != 412.55 || g.PowerLimitW != 600 {
		t.Errorf("telemetry mismatch: %+v", g)
	}
	if g.PCIeGen != 5 || g.PCIeWidth != 16 {
		t.Errorf("pcie link = gen%d x%d, want gen5 x16", g.PCIeGen, g.PCIeWidth)
	}
	if g.DriverVersion != "565.57.01" {
		t.Errorf("driver = %q", g.DriverVersion)
	}
	// "Not Active" is not a throttle reason.
	if len(g.ThrottleReasons) != 0 {
		t.Errorf("throttle reasons = %v, want none", g.ThrottleReasons)
	}
}

// TestParseSMIDegradedFields: nvidia-smi emits [N/A] per FIELD on cards that do
// not expose a sensor. One unreadable field must not lose the whole GPU —
// temperature and power are the signals that matter most and are almost always
// present.
func TestParseSMIDegradedFields(t *testing.T) {
	line := `0, GPU-aaaa, NVIDIA RTX PRO 6000, 45, 1024, 98304, 68, 250.10, [N/A], [Not Supported], 1800, [N/A], [N/A], [N/A], Not Active, 565.57.01`
	gpus, err := parseSMI(line)
	if err != nil {
		t.Fatal(err)
	}
	g := gpus[0]
	if g.TempC != 68 || g.PowerW != 250.10 {
		t.Errorf("the readable fields must survive: %+v", g)
	}
	if g.PowerLimitW != 0 || g.FanPct != 0 || g.PCIeGen != 0 {
		t.Errorf("unreadable fields should be zero, not garbage: %+v", g)
	}
}

// TestParseSMIThrottling: a GPU in a homeowner's cabinet with the vents against
// a wall throttles for reasons that matter, and "gpu idle" is not one of them.
func TestParseSMIThrottling(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"Not Active", nil},
		{"[N/A]", nil},
		{"0x0000000000000000", nil},
		{"GPU Idle", nil},
		{"SW Thermal Slowdown", []string{"sw_thermal_slowdown"}},
		{"HW Power Brake Slowdown; SW Power Cap", []string{"hw_power_brake_slowdown", "sw_power_cap"}},
	}
	for _, c := range cases {
		got := parseThrottleReasons(c.raw)
		if len(got) != len(c.want) {
			t.Errorf("parseThrottleReasons(%q) = %v, want %v", c.raw, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseThrottleReasons(%q) = %v, want %v", c.raw, got, c.want)
			}
		}
	}
}

func TestParseSMIECCErrors(t *testing.T) {
	line := `0, GPU-aaaa, NVIDIA RTX PRO 6000, 10, 1024, 98304, 55, 90.0, 600.00, 40, 1400, 5, 16, 7, Not Active, 565.57.01`
	gpus, err := parseSMI(line)
	if err != nil {
		t.Fatal(err)
	}
	if gpus[0].ECCErrors != 7 {
		t.Fatalf("ecc errors = %d, want 7", gpus[0].ECCErrors)
	}
}

func TestParseSMIMultipleGPUs(t *testing.T) {
	out := smiHealthy + "\n" +
		`1, GPU-2b3c4d5e-6f70-8192-a3b4-c5d6e7f8091a, NVIDIA RTX PRO 6000 Blackwell, 12, 800, 98304, 48, 95.20, 600.00, 30, 900, 5, 16, 0, Not Active, 565.57.01` + "\n"
	gpus, err := parseSMI(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 2 || gpus[0].Index != 0 || gpus[1].Index != 1 {
		t.Fatalf("got %+v", gpus)
	}
}

func TestParseSMIRejectsGarbage(t *testing.T) {
	for _, bad := range []string{
		"not, enough, fields",
		`0, , NVIDIA RTX PRO 6000, 45, 1024, 98304, 68, 250, 600, 50, 1800, 5, 16, 0, Not Active, 565.57.01`, // no UUID
	} {
		if _, err := parseSMI(bad); err == nil {
			t.Errorf("parseSMI(%q) should fail", bad)
		}
	}
	// Empty output is not an error at the parser level; the collector turns
	// "no GPUs" into one.
	if gpus, err := parseSMI("\n\n"); err != nil || len(gpus) != 0 {
		t.Errorf("blank output: got %v, %v", gpus, err)
	}
}

// --- degraded-path behaviour ---------------------------------------------

type fakeSource struct {
	name      string
	available bool
	gpus      []proto.GPU
	err       error
	reads     int
}

func (f *fakeSource) Name() string                       { return f.name }
func (f *fakeSource) Available(ctx context.Context) bool { return f.available }
func (f *fakeSource) Read(ctx context.Context) ([]proto.GPU, error) {
	f.reads++
	return f.gpus, f.err
}

// TestGPUCollectorFallsBackWhenNVMLIsUnavailable: this is the driver-upgrade
// case. NVML's shared library and the kernel module disagree for a minute or
// two during an upgrade; the GPU is fine and the node must stay in the fleet.
func TestGPUCollectorFallsBackWhenPreferredSourceIsUnavailable(t *testing.T) {
	nvml := &fakeSource{name: "nvml", available: false}
	smi := &fakeSource{name: "nvidia-smi", available: true, gpus: []proto.GPU{{UUID: "GPU-a", TempC: 60}}}

	g := NewGPUCollector(nvml, smi)
	var s proto.Sample
	if err := g.Collect(context.Background(), &s); err != nil {
		t.Fatalf("the collector must fall back rather than fail: %v", err)
	}
	if len(s.GPUs) != 1 || s.GPUs[0].UUID != "GPU-a" {
		t.Fatalf("got %+v", s.GPUs)
	}
	if g.ActiveSource() != "nvidia-smi" {
		t.Errorf("active source = %q, want nvidia-smi", g.ActiveSource())
	}
	if nvml.reads != 0 {
		t.Error("an unavailable source must not be read")
	}
}

// TestGPUCollectorReportsNoDriverWithoutFabricatingData is ADR 0003 at the
// agent end: no driver means Sample.GPUs stays nil, never a zero-valued GPU.
func TestGPUCollectorReportsNoDriver(t *testing.T) {
	g := NewGPUCollector(&fakeSource{name: "nvml", available: false})

	var s proto.Sample
	err := g.Collect(context.Background(), &s)
	if !errors.Is(err, ErrNoDriver) {
		t.Fatalf("got %v, want ErrNoDriver", err)
	}
	if s.GPUs != nil {
		t.Fatalf("no driver must leave GPUs nil, got %+v", s.GPUs)
	}
	if g.ActiveSource() != "none" {
		t.Errorf("active source = %q, want none", g.ActiveSource())
	}
}

// TestGPUCollectorRePromotesAfterAFailedRead: a driver that vanishes mid-run
// must promote the fallback on the NEXT sample, not after the probe interval.
func TestGPUCollectorRePromotesAfterAFailedRead(t *testing.T) {
	nvml := &fakeSource{name: "nvml", available: true, err: errors.New("library/kernel module mismatch")}
	smi := &fakeSource{name: "nvidia-smi", available: true, gpus: []proto.GPU{{UUID: "GPU-a"}}}

	g := NewGPUCollector(nvml, smi)

	var s1 proto.Sample
	if err := g.Collect(context.Background(), &s1); err == nil {
		t.Fatal("the first read should surface the failure")
	}

	// NVML has now gone away entirely; the next sample must use the fallback
	// without waiting out the probe interval.
	nvml.available = false
	var s2 proto.Sample
	if err := g.Collect(context.Background(), &s2); err != nil {
		t.Fatalf("the next sample should use the fallback: %v", err)
	}
	if len(s2.GPUs) != 1 {
		t.Fatalf("got %+v", s2.GPUs)
	}
}

func TestGPUCollectorTreatsNoGPUsAsAnError(t *testing.T) {
	// A source that works but reports nothing means the card fell off the bus,
	// which is a fault — not an absence of hardware on a machine we shipped
	// with a GPU in it.
	g := NewGPUCollector(&fakeSource{name: "nvml", available: true, gpus: nil})
	var s proto.Sample
	if err := g.Collect(context.Background(), &s); err == nil {
		t.Fatal("a source reporting zero GPUs must be an error")
	}
}
