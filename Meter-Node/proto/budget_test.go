package meternodeproto

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// TestHeartbeatFitsBudget pins the contract's "< 100 bytes" heartbeat.
//
// This test is the reason nobody can casually add a field to Heartbeat: at a
// 15 s interval a single extra byte costs ~172 KB/month/node, and the whole
// control-plane budget is 150 MB.
func TestHeartbeatFitsBudget(t *testing.T) {
	env, err := NewEnvelope("6f1b8a3e-0c0d-4c1e-9a3b-1f2e3d4c5b6a", 1<<40, time.Now().UnixMilli(), KindHeartbeat, Heartbeat{
		UptimeS:     86400 * 30,
		ClockSkewMS: -1234,
		Degraded:    true,
	})
	if err != nil {
		t.Fatalf("build heartbeat: %v", err)
	}
	wire, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(wire) > HeartbeatMaxBytes {
		t.Fatalf("heartbeat envelope is %d bytes, budget is %d", len(wire), HeartbeatMaxBytes)
	}
	t.Logf("heartbeat wire size: %d bytes (budget %d)", len(wire), HeartbeatMaxBytes)

	if env.C != CompressionNone {
		t.Fatalf("heartbeat should not be compressed, got %s", env.C)
	}
}

// realisticSample builds a sample resembling a live RTX PRO node: one GPU
// under load, two mounts, a busy NIC. Values jitter because zstd would flatter
// us unfairly on a batch of identical samples.
func realisticSample(rng *rand.Rand, t time.Time, gpus int) Sample {
	smartOK := true
	s := Sample{
		T: t.UnixMilli(),
		CPU: &CPU{
			UtilPct: 20 + rng.Float32()*40, Load1: 1 + rng.Float32()*6,
			TempC: 45 + rng.Float32()*20, FreqMHz: uint32(3200 + rng.Intn(1400)),
		},
		Mem: &Mem{UsedMB: uint32(18000 + rng.Intn(9000)), TotalMB: 65536, SwapUsedMB: uint32(rng.Intn(512))},
		Disk: []Disk{
			{Mount: "/", UsedGB: 210 + rng.Float32(), TotalGB: 1863, ReadMBps: rng.Float32() * 300, WriteMBps: rng.Float32() * 180, SMARTOK: &smartOK},
			{Mount: "/var/lib/docker", UsedGB: 640 + rng.Float32(), TotalGB: 3726, ReadMBps: rng.Float32() * 900, WriteMBps: rng.Float32() * 400, SMARTOK: &smartOK},
		},
		Net: &Net{
			RxMbps: rng.Float32() * 240, TxMbps: rng.Float32() * 28,
			RxTotalGBMonth: 412.5, TxTotalGBMonth: 88.25,
			ControlPlaneBytes: uint64(rng.Intn(1 << 26)), Iface: "enp5s0",
		},
		Host: &Host{
			UptimeS: uint64(rng.Intn(1 << 21)), AgentVersion: "1.0.0",
			Kernel: "6.8.0-45-generic", OS: "Ubuntu 24.04.1 LTS",
			BootID: "c1a2b3c4-d5e6-4778-89ab-cdef01234567", ClockSkewMS: int32(rng.Intn(80) - 40),
		},
	}
	for i := 0; i < gpus; i++ {
		s.GPUs = append(s.GPUs, GPU{
			Index:   uint8(i),
			UUID:    fmt.Sprintf("GPU-1a2b3c4d-5e6f-7081-92a3-b4c5d6e7f80%d", i),
			Name:    "NVIDIA RTX PRO 6000 Blackwell",
			UtilPct: 60 + rng.Float32()*40, MemUsedMB: uint32(20000 + rng.Intn(20000)), MemTotalMB: 98304,
			TempC: 62 + rng.Float32()*18, PowerW: 300 + rng.Float32()*300, PowerLimitW: 600,
			FanPct: 45 + rng.Float32()*40, SMClockMHz: uint32(1800 + rng.Intn(700)),
			PCIeGen: 5, PCIeWidth: 16, ECCErrors: 0, DriverVersion: "565.57.01",
		})
	}
	return s
}

// TestMonthlyBudget is the load-bearing test of the whole transport design.
//
// It encodes a full month of control-plane traffic exactly as the agent would
// send it — a heartbeat every 15 s and a compressed 6-sample batch every 60 s
// — and asserts the total lands under the 150 MB target, not merely under the
// 500 MB ceiling. Framing overhead (TLS records, WebSocket headers) is added
// as a flat per-message estimate so the number is not optimistic.
func TestMonthlyBudget(t *testing.T) {
	const (
		// TLS record + WebSocket frame header, generously rounded up. Real
		// overhead on a TLS 1.3 record is 5 + 16 bytes plus a 2-14 byte WS
		// header; 40 covers the worst case with room to spare.
		perMessageOverhead = 40
	)

	for _, gpus := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("%dgpu", gpus), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(gpus)))
			now := time.Now()

			hb, err := NewEnvelope("6f1b8a3e-0c0d-4c1e-9a3b-1f2e3d4c5b6a", 1, now.UnixMilli(), KindHeartbeat, Heartbeat{UptimeS: 12345, ClockSkewMS: 12})
			if err != nil {
				t.Fatal(err)
			}
			hbWire, _ := MarshalEnvelope(hb)
			hbBytes := uint64(len(hbWire) + perMessageOverhead)

			// Encode a representative spread of batches rather than one, so a
			// single lucky compression ratio cannot carry the result.
			const trials = 64
			var batchTotal uint64
			perFlush := int(DefaultFlushInterval / DefaultSampleInterval)
			for i := 0; i < trials; i++ {
				batch := MetricBatch{}
				for j := 0; j < perFlush; j++ {
					batch.Samples = append(batch.Samples, realisticSample(rng, now.Add(time.Duration(j)*DefaultSampleInterval), gpus))
				}
				env, err := NewEnvelope("6f1b8a3e-0c0d-4c1e-9a3b-1f2e3d4c5b6a", uint64(i), now.UnixMilli(), KindMetrics, batch)
				if err != nil {
					t.Fatal(err)
				}
				if env.C != CompressionZstd {
					t.Fatalf("metric batch should compress, got %s", env.C)
				}
				wire, _ := MarshalEnvelope(env)
				batchTotal += uint64(len(wire) + perMessageOverhead)
			}
			batchBytes := batchTotal / trials

			hbPerMonth := uint64(AverageMonth / DefaultHeartbeatInterval)
			flushPerMonth := uint64(AverageMonth / DefaultFlushInterval)
			total := hbBytes*hbPerMonth + batchBytes*flushPerMonth

			t.Logf("%d GPU: heartbeat %d B x %d = %.1f MB; batch %d B x %d = %.1f MB; total %.1f MB (target %d MB)",
				gpus, hbBytes, hbPerMonth, float64(hbBytes*hbPerMonth)/(1<<20),
				batchBytes, flushPerMonth, float64(batchBytes*flushPerMonth)/(1<<20),
				float64(total)/(1<<20), TargetMonthlyBytes/(1<<20))

			if total > TargetMonthlyBytes {
				t.Errorf("projected %.1f MB/month exceeds the %d MB target", float64(total)/(1<<20), TargetMonthlyBytes/(1<<20))
			}
			if total > CeilingMonthlyBytes {
				t.Fatalf("projected %.1f MB/month exceeds the %d MB HARD CEILING", float64(total)/(1<<20), CeilingMonthlyBytes/(1<<20))
			}
		})
	}
}

func TestClassifyBudget(t *testing.T) {
	const target = TargetMonthlyBytes
	cases := []struct {
		name string
		mtd  uint64
		want BudgetStatus
	}{
		{"fresh month", 0, BudgetOK},
		{"half", target / 2, BudgetOK},
		{"three quarters", uint64(float64(target) * DegradeAtFraction), BudgetApproaching},
		{"at target", target, BudgetOverTarget},
		{"at ceiling", CeilingMonthlyBytes, BudgetOverCeiling},
		{"way over", 2 * CeilingMonthlyBytes, BudgetOverCeiling},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyBudget(c.mtd, target); got != c.want {
				t.Fatalf("ClassifyBudget(%d) = %s, want %s", c.mtd, got, c.want)
			}
		})
	}
}

func TestProjectedMonthlyBytes(t *testing.T) {
	// A day at 5 MB projects to 150 MB, which is exactly the target: a useful
	// mental anchor for anyone reading a node's bandwidth panel.
	got := ProjectedMonthlyBytes(5*1024*1024, 24*time.Hour)
	if want := uint64(150 * 1024 * 1024); got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got := ProjectedMonthlyBytes(1234, 0); got != 0 {
		t.Fatalf("zero window should project 0, got %d", got)
	}
}
