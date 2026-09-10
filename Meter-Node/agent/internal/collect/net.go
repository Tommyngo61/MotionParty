package collect

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
	gnet "github.com/shirou/gopsutil/v3/net"
)

// ControlPlaneCounter reports how many bytes the agent itself has spent talking
// to the controller this month.
//
// This is a first-class metric, not instrumentation. The whole premise of
// putting company hardware in someone's house is that our traffic is a rounding
// error against their ~1.2 TB cap — and some residential ISPs prohibit
// commercial use, so this is the number that makes the argument measurable
// rather than asserted. The transport implements it (M4); the collector reads
// it through this interface so the two do not depend on each other.
type ControlPlaneCounter interface {
	// ControlPlaneBytes is month-to-date bytes sent and received on the
	// control plane.
	ControlPlaneBytes() uint64
	// MonthToDate is the host interface totals since the billing cycle reset,
	// in bytes. Zero means not yet known.
	MonthToDate() (rx, tx uint64)
}

// NetCollector reports interface throughput and standing against the data cap.
type NetCollector struct {
	iface   string
	counter ControlPlaneCounter

	mu       sync.Mutex
	lastRx   uint64
	lastTx   uint64
	lastRead time.Time
	resolved string
}

// NewNetCollector builds the collector. iface may be empty, in which case the
// interface carrying the default route is used — that is the one the
// homeowner's ISP meters. counter may be nil before the transport exists.
func NewNetCollector(iface string, counter ControlPlaneCounter) *NetCollector {
	return &NetCollector{iface: iface, counter: counter}
}

func (n *NetCollector) Name() string { return "net" }

func (n *NetCollector) Collect(ctx context.Context, s *proto.Sample) error {
	iface := n.iface
	if iface == "" {
		var err error
		if iface, err = n.defaultRouteInterface(); err != nil {
			return err
		}
	}

	counters, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return fmt.Errorf("collect: read network counters: %w", err)
	}

	var stat *gnet.IOCountersStat
	for i := range counters {
		if counters[i].Name == iface {
			stat = &counters[i]
			break
		}
	}
	if stat == nil {
		return fmt.Errorf("collect: interface %q not found", iface)
	}

	out := &proto.Net{Iface: iface}

	n.mu.Lock()
	now := time.Now()
	if !n.lastRead.IsZero() {
		if elapsed := now.Sub(n.lastRead).Seconds(); elapsed > 0 {
			// Counters reset on interface down/up, which on a residential link
			// happens every time the ISP blips. A negative delta is that, not
			// a real rate.
			if stat.BytesRecv >= n.lastRx {
				out.RxMbps = float32(float64(stat.BytesRecv-n.lastRx) * 8 / elapsed / 1e6)
			}
			if stat.BytesSent >= n.lastTx {
				out.TxMbps = float32(float64(stat.BytesSent-n.lastTx) * 8 / elapsed / 1e6)
			}
		}
	}
	n.lastRx, n.lastTx, n.lastRead = stat.BytesRecv, stat.BytesSent, now
	n.mu.Unlock()

	if n.counter != nil {
		out.ControlPlaneBytes = n.counter.ControlPlaneBytes()
		rx, tx := n.counter.MonthToDate()
		out.RxTotalGBMonth = float32(rx) / (1024 * 1024 * 1024)
		out.TxTotalGBMonth = float32(tx) / (1024 * 1024 * 1024)
	}

	s.Net = out
	return nil
}

// defaultRouteInterface finds the interface carrying the default route.
//
// Read from /proc/net/route rather than shelling out to `ip route`: the agent
// runs unprivileged and must not depend on iproute2 being present in whatever
// image the node was built from.
func (n *NetCollector) defaultRouteInterface() (string, error) {
	n.mu.Lock()
	cached := n.resolved
	n.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", fmt.Errorf("collect: read routing table: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		// Iface Destination Gateway Flags ...; destination 00000000 is the
		// default route.
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		n.mu.Lock()
		n.resolved = fields[0]
		n.mu.Unlock()
		return fields[0], nil
	}
	return "", fmt.Errorf("collect: no default route found")
}

// ResetInterface clears the cached default-route interface, so a node whose
// homeowner swapped from wired to wireless picks up the new one.
func (n *NetCollector) ResetInterface() {
	n.mu.Lock()
	n.resolved = ""
	n.mu.Unlock()
}
