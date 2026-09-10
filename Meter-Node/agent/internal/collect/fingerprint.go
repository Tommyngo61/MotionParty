package collect

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"

	proto "github.com/MeterHome/Meter-Node/proto"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// Fingerprint reads the hardware identity this node's credential is bound to.
//
// Every source here is readable without privileges on the Ubuntu image we ship:
// DMI comes from sysfs (world-readable for product_uuid on our image; see
// SECURITY.md), GPU UUIDs from nvidia-smi, and the MAC from the netlink
// interface list. Nothing shells out to dmidecode, which would need root.
//
// A component the agent cannot read leaves an empty string rather than an
// error. The controller decides whether what it got is enough — see
// HardwareFingerprint.Complete — because "this board reports a placeholder
// UUID" is a fleet-wide policy question, not something a single node should
// resolve for itself.
func Fingerprint(ctx context.Context, gpu *GPUCollector) (proto.HardwareFingerprint, error) {
	fp := proto.HardwareFingerprint{
		MotherboardUUID: readDMI("product_uuid"),
		PrimaryNICMAC:   primaryMAC(),
	}

	if gpu != nil {
		var s proto.Sample
		// A GPU read failure must not block enrollment. A node whose driver is
		// mid-upgrade should still be able to join; the fingerprint is then
		// weaker, and the controller records that rather than assuming it.
		if err := gpu.Collect(ctx, &s); err == nil {
			for _, g := range s.GPUs {
				fp.GPUUUIDs = append(fp.GPUUUIDs, g.UUID)
			}
			sort.Strings(fp.GPUUUIDs)
		}
	}

	if fp.MotherboardUUID == "" && len(fp.GPUUUIDs) == 0 && fp.PrimaryNICMAC == "" {
		// Nothing at all. Enrolling on this would bind an identity to a hash of
		// three empty strings — which every such node would share.
		return fp, fmt.Errorf("collect: could not read any hardware identifier")
	}
	return fp, nil
}

// dmiPaths are the sysfs locations for the board's DMI data, in preference
// order. product_uuid is the DMI system UUID; board_serial is a weaker fallback
// for boards that hide it.
func readDMI(field string) string {
	for _, base := range []string{"/sys/class/dmi/id/", "/sys/devices/virtual/dmi/id/"} {
		if v := readTrimmed(base + field); v != "" {
			return v
		}
	}
	return ""
}

// primaryMAC returns the MAC of the interface carrying the default route.
//
// Not "the first non-loopback interface": a node with Docker installed has
// docker0, br-*, and a veth per container, and picking one of those would make
// the fingerprint change every time the container set changed — which would
// look like a hardware swap and drag a healthy node into re-attestation.
func primaryMAC() string {
	iface := defaultRouteIface()
	if iface != "" {
		if hw := macOf(iface); hw != "" {
			return hw
		}
	}

	// No default route (a node that boots before the homeowner's router is up).
	// Fall back to the lowest-numbered physical interface, which is stable
	// across reboots even though it is a weaker signal.
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Index < ifaces[j].Index })
	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 || len(i.HardwareAddr) == 0 || isVirtualIface(i.Name) {
			continue
		}
		return i.HardwareAddr.String()
	}
	return ""
}

// isVirtualIface filters the interfaces a container host grows.
func isVirtualIface(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "tun", "tap", "wg"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func defaultRouteIface() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "00000000" {
			return fields[0]
		}
	}
	return ""
}

func macOf(name string) string {
	iface, err := net.InterfaceByName(name)
	if err != nil || len(iface.HardwareAddr) == 0 {
		return ""
	}
	return iface.HardwareAddr.String()
}

// Inventory collects the static machine record sent once at enrollment.
//
// This is what an operator looks at to answer "what is actually in that box"
// without waiting for a live sample from a node that may be unplugged.
func Inventory(ctx context.Context, gpu *GPUCollector) proto.HardwareInventory {
	inv := proto.HardwareInventory{
		OS:           runtime.GOOS,
		Kernel:       readTrimmed("/proc/sys/kernel/osrelease"),
		BoardVendor:  readDMI("board_vendor"),
		BoardProduct: readDMI("board_name"),
	}

	if hostname, err := os.Hostname(); err == nil {
		inv.Hostname = hostname
	}
	if info, err := host.InfoWithContext(ctx); err == nil {
		inv.OS = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
		if info.KernelVersion != "" {
			inv.Kernel = info.KernelVersion
		}
	}

	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		inv.CPUModel = infos[0].ModelName
		inv.CPUThreads = uint16(len(infos))
		if n, err := cpu.CountsWithContext(ctx, false); err == nil {
			inv.CPUCores = uint16(n)
		}
		// gopsutil reports one entry per logical CPU on some kernels and one
		// per socket on others. Cores never exceeding threads is the invariant
		// worth holding onto.
		if inv.CPUCores == 0 || inv.CPUCores > inv.CPUThreads {
			inv.CPUCores = inv.CPUThreads
		}
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		inv.MemTotalMB = uint32(vm.Total / (1024 * 1024))
	}

	if gpu != nil {
		var s proto.Sample
		if err := gpu.Collect(ctx, &s); err == nil {
			for _, g := range s.GPUs {
				inv.GPUs = append(inv.GPUs, proto.GPUInventory{
					Index: g.Index, UUID: g.UUID, Name: g.Name,
					MemTotalMB: g.MemTotalMB, DriverVersion: g.DriverVersion,
				})
			}
		}
	}

	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		seen := map[string]bool{}
		for _, p := range parts {
			dev := deviceOf(p.Device)
			if pseudoFilesystems[p.Fstype] || dev == "" || seen[dev] {
				continue
			}
			seen[dev] = true
			d := proto.DiskInventory{Device: dev}
			if u, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil {
				d.SizeGB = uint32(u.Total / (1024 * 1024 * 1024))
			}
			inv.Disks = append(inv.Disks, d)
		}
		sort.Slice(inv.Disks, func(i, j int) bool { return inv.Disks[i].Device < inv.Disks[j].Device })
	}

	return inv
}
