package meternodeproto

// MetricBatch is the payload of a KindMetrics envelope: one flush interval's
// worth of samples.
//
// At the contract's 10 s sampling and 60 s flush this holds 6 samples. On
// reconnect after an outage it holds downsampled backfill instead, and
// Backfill is set so the controller can tell a live batch from a catch-up
// batch when it scores health and when it evaluates alert rules — a rule must
// not fire on data that is an hour old just because it arrived now.
type MetricBatch struct {
	Samples []Sample `msgpack:"samples"`

	// Backfill marks a batch replayed from the agent's local buffer rather
	// than sampled in the last interval.
	Backfill bool `msgpack:"backfill,omitempty"`

	// DownsampleFactor is how many raw samples each carried sample
	// represents. 0 or 1 means full resolution. The agent raises this when
	// backfilling or when the bandwidth cap forces degradation, and the
	// controller uses it to weight aggregates honestly.
	DownsampleFactor uint16 `msgpack:"downsample,omitempty"`
}

// Sample is one point-in-time reading of the whole node.
//
// Field groups are pointers where a collector can legitimately be absent:
// a node mid-driver-upgrade has no GPU data, and encoding a zero-valued GPU
// block would be indistinguishable from a GPU sitting idle at 0 W. Absent and
// zero must never collapse into each other in this schema.
type Sample struct {
	T          int64       `msgpack:"t"` // unix ms
	CPU        *CPU        `msgpack:"cpu,omitempty"`
	Mem        *Mem        `msgpack:"mem,omitempty"`
	Disk       []Disk      `msgpack:"disk,omitempty"`
	GPUs       []GPU       `msgpack:"gpus,omitempty"`
	Net        *Net        `msgpack:"net,omitempty"`
	Host       *Host       `msgpack:"host,omitempty"`
	Containers []Container `msgpack:"containers,omitempty"`

	// CollectorErrors names collectors that failed or timed out for this
	// sample. A hung disk or a wedged NVML call must never stall the agent,
	// so the collector is abandoned and its failure recorded here rather than
	// the whole sample being dropped.
	CollectorErrors []string `msgpack:"collector_errors,omitempty"`
}

// CPU holds host processor state.
type CPU struct {
	UtilPct   float32 `msgpack:"util_pct"`
	Load1     float32 `msgpack:"load1"`
	TempC     float32 `msgpack:"temp_c"`
	FreqMHz   uint32  `msgpack:"freq_mhz"`
	Throttled bool    `msgpack:"throttled,omitempty"`
}

// Mem holds host memory state. Megabytes, not bytes: a node with 128 GiB of
// RAM still fits comfortably in a uint32 and the smaller varint saves bytes on
// every sample.
type Mem struct {
	UsedMB     uint32 `msgpack:"used_mb"`
	TotalMB    uint32 `msgpack:"total_mb"`
	SwapUsedMB uint32 `msgpack:"swap_used_mb"`
}

// Disk holds one mount's state.
//
// SMARTOK is a pointer because "we could not read SMART" (a USB bridge, a
// virtualised disk, a missing permission) is a different fact from "SMART says
// this disk is failing", and only the second should raise an alert.
type Disk struct {
	Mount     string  `msgpack:"mount"`
	UsedGB    float32 `msgpack:"used_gb"`
	TotalGB   float32 `msgpack:"total_gb"`
	ReadMBps  float32 `msgpack:"read_mbps"`
	WriteMBps float32 `msgpack:"write_mbps"`
	SMARTOK   *bool   `msgpack:"smart_ok,omitempty"`
}

// GPU holds one NVIDIA device's state, as reported by NVML with a nvidia-smi
// fallback.
//
// PCIeGen and PCIeWidth are the *current* link state, not the maximum: PCIe
// downtraining under a hot residential desk is one of the degradation signals
// the controller scores on, and it is invisible if only the capability is
// reported.
type GPU struct {
	Index           uint8    `msgpack:"index"`
	UUID            string   `msgpack:"uuid"`
	Name            string   `msgpack:"name"`
	UtilPct         float32  `msgpack:"util_pct"`
	MemUsedMB       uint32   `msgpack:"mem_used_mb"`
	MemTotalMB      uint32   `msgpack:"mem_total_mb"`
	TempC           float32  `msgpack:"temp_c"`
	PowerW          float32  `msgpack:"power_w"`
	PowerLimitW     float32  `msgpack:"power_limit_w"`
	FanPct          float32  `msgpack:"fan_pct"`
	SMClockMHz      uint32   `msgpack:"sm_clock_mhz"`
	PCIeGen         uint8    `msgpack:"pcie_gen"`
	PCIeWidth       uint8    `msgpack:"pcie_width"`
	ECCErrors       uint64   `msgpack:"ecc_errors"`
	ThrottleReasons []string `msgpack:"throttle_reasons,omitempty"`
	DriverVersion   string   `msgpack:"driver_version"`
}

// Net holds interface throughput and the node's standing against its data cap.
//
// ControlPlaneBytes is the agent's own accounting of every byte it has spent
// talking to the controller this month. It is a first-class metric because the
// whole product depends on staying a rounding error against a residential
// ~1.2 TB cap, and because some ISPs prohibit commercial use — this is the
// number that makes that argument measurable rather than asserted.
type Net struct {
	RxMbps            float32 `msgpack:"rx_mbps"`
	TxMbps            float32 `msgpack:"tx_mbps"`
	RxTotalGBMonth    float32 `msgpack:"rx_total_gb_month"`
	TxTotalGBMonth    float32 `msgpack:"tx_total_gb_month"`
	ControlPlaneBytes uint64  `msgpack:"control_plane_bytes"`
	Iface             string  `msgpack:"iface"`
}

// Host holds process and OS identity for the sample.
//
// BootID changes on every host boot. Together with UptimeS it separates "the
// homeowner power-cycled the machine" from "the agent crashed and systemd
// restarted it", which are very different problems and must not share an
// alert.
type Host struct {
	UptimeS      uint64 `msgpack:"uptime_s"`
	AgentVersion string `msgpack:"agent_version"`
	Kernel       string `msgpack:"kernel"`
	OS           string `msgpack:"os"`
	BootID       string `msgpack:"boot_id"`
	ClockSkewMS  int32  `msgpack:"clock_skew_ms"`
}

// Container holds one workload container's state.
//
// In phase 1 the desired set of workloads is always empty, so this is normally
// nil. The field exists now so the schema does not need a version bump when
// workloads arrive.
type Container struct {
	ID       string  `msgpack:"id"`
	Image    string  `msgpack:"image"`
	State    string  `msgpack:"state"`
	CPUPct   float32 `msgpack:"cpu_pct"`
	MemMB    uint32  `msgpack:"mem_mb"`
	Restarts uint32  `msgpack:"restarts"`
}
