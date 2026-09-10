package collect

import (
	"context"
	"fmt"

	proto "github.com/MeterHome/Meter-Node/proto"
	"github.com/shirou/gopsutil/v3/mem"
)

// MemCollector reports memory state.
type MemCollector struct{}

func NewMemCollector() *MemCollector { return &MemCollector{} }

func (m *MemCollector) Name() string { return "mem" }

func (m *MemCollector) Collect(ctx context.Context, s *proto.Sample) error {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return fmt.Errorf("collect: read memory: %w", err)
	}

	out := &proto.Mem{
		// gopsutil's Used already excludes buffers and cache on Linux, which
		// is what an operator means by "used". Total minus Available would
		// report a healthy node with a warm page cache as nearly full.
		UsedMB:  uint32(vm.Used / (1024 * 1024)),
		TotalMB: uint32(vm.Total / (1024 * 1024)),
	}

	// Swap is best-effort: a node imaged without swap is normal, not broken.
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		out.SwapUsedMB = uint32(sw.Used / (1024 * 1024))
	}

	s.Mem = out
	return nil
}
