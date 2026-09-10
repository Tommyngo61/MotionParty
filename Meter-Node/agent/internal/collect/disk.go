package collect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
	"github.com/shirou/gopsutil/v3/disk"
)

// pseudoFilesystems are mounts that are not storage.
//
// A container host accumulates dozens of these. Reporting them would triple the
// size of every sample — a real cost against a 150 MB/month budget — to tell an
// operator that a tmpfs is 0% full.
var pseudoFilesystems = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "devpts": true, "sysfs": true, "proc": true,
	"cgroup": true, "cgroup2": true, "overlay": true, "squashfs": true,
	"ramfs": true, "securityfs": true, "debugfs": true, "tracefs": true,
	"pstore": true, "bpf": true, "configfs": true, "fusectl": true,
	"hugetlbfs": true, "mqueue": true, "autofs": true, "binfmt_misc": true,
	"efivarfs": true, "nsfs": true, "fuse.snapfuse": true, "rpc_pipefs": true,
}

// DiskCollector reports per-mount usage and throughput.
//
// This is the collector most likely to hang, and the reason the whole registry
// is built around timeouts: a statfs against a dying consumer SSD or a USB
// enclosure that has stopped answering can block for tens of seconds in
// uninterruptible sleep. Disks filling up is also the top real-world failure
// for a container host, so it cannot simply be dropped.
type DiskCollector struct {
	// mounts pins collection to specific paths. Empty means every real
	// filesystem.
	mounts []string

	mu       sync.Mutex
	lastIO   map[string]disk.IOCountersStat
	lastRead time.Time
}

// NewDiskCollector builds the collector. mounts may be nil.
func NewDiskCollector(mounts []string) *DiskCollector {
	return &DiskCollector{mounts: mounts, lastIO: map[string]disk.IOCountersStat{}}
}

func (d *DiskCollector) Name() string { return "disk" }

func (d *DiskCollector) Collect(ctx context.Context, s *proto.Sample) error {
	partitions, err := d.partitions(ctx)
	if err != nil {
		return err
	}

	// Throughput is a delta against the previous sample. The first sample
	// after start reports no rates rather than a bogus rate computed against
	// the counters' value since boot, which on a node with months of uptime
	// would be a wildly wrong number on the very first data point a new node
	// sends.
	rates := d.ioRates(ctx)

	var out []proto.Disk
	var errs []string
	for _, p := range partitions {
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			// One unreadable mount must not lose the others. A failing USB
			// enclosure is exactly the case where the internal NVMe's numbers
			// still matter.
			errs = append(errs, p.Mountpoint)
			continue
		}
		entry := proto.Disk{
			Mount:   p.Mountpoint,
			UsedGB:  float32(usage.Used) / (1024 * 1024 * 1024),
			TotalGB: float32(usage.Total) / (1024 * 1024 * 1024),
		}
		if r, ok := rates[deviceOf(p.Device)]; ok {
			entry.ReadMBps, entry.WriteMBps = r[0], r[1]
		}
		// SMART is deliberately left nil here rather than guessed at. NULL
		// means "could not be read", which is a different fact from "SMART
		// says this disk is failing", and only the second should raise an
		// alert. Reading it needs privileged ioctls the agent does not hold;
		// see SECURITY.md.
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	s.Disk = out

	if len(errs) > 0 {
		return fmt.Errorf("collect: could not stat %d mount(s): %s", len(errs), strings.Join(errs, ", "))
	}
	return nil
}

func (d *DiskCollector) partitions(ctx context.Context) ([]disk.PartitionStat, error) {
	if len(d.mounts) > 0 {
		out := make([]disk.PartitionStat, 0, len(d.mounts))
		for _, m := range d.mounts {
			out = append(out, disk.PartitionStat{Mountpoint: m})
		}
		return out, nil
	}

	// all=false asks gopsutil to skip pseudo filesystems, but it is not
	// exhaustive across kernel versions, so the explicit list above filters
	// what gets through.
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("collect: list partitions: %w", err)
	}

	seen := map[string]bool{}
	var out []disk.PartitionStat
	for _, p := range parts {
		if pseudoFilesystems[p.Fstype] || seen[p.Mountpoint] {
			continue
		}
		// Bind mounts and container layers of the same device would otherwise
		// be reported several times over.
		seen[p.Mountpoint] = true
		out = append(out, p)
	}
	return out, nil
}

// ioRates returns MB/s read and write per device since the last call.
func (d *DiskCollector) ioRates(ctx context.Context) map[string][2]float32 {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	rates := map[string][2]float32{}
	if !d.lastRead.IsZero() {
		if elapsed := now.Sub(d.lastRead).Seconds(); elapsed > 0 {
			for name, c := range counters {
				prev, ok := d.lastIO[name]
				if !ok || c.ReadBytes < prev.ReadBytes {
					// Counter went backwards: the device was replaced or the
					// host rebooted. Skip rather than report a negative rate.
					continue
				}
				rates[name] = [2]float32{
					float32(float64(c.ReadBytes-prev.ReadBytes) / elapsed / (1024 * 1024)),
					float32(float64(c.WriteBytes-prev.WriteBytes) / elapsed / (1024 * 1024)),
				}
			}
		}
	}

	d.lastIO = counters
	d.lastRead = now
	return rates
}

// deviceOf maps /dev/nvme0n1p2 to the counter key gopsutil uses.
func deviceOf(devPath string) string {
	return strings.TrimPrefix(devPath, "/dev/")
}
