package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

func (p procFS) Machine(ctx context.Context) (model.MachineSample, error) {
	s := model.MachineSample{PressureLevel: "unknown", TS: time.Now(), Cores: runtime.NumCPU()}
	if err := ctx.Err(); err != nil {
		return s, err
	}
	ctx = p.context(ctx)
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return s, fmt.Errorf("virtual memory: %w", err)
	}
	swap, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return s, fmt.Errorf("swap memory: %w", err)
	}
	if vm.Total == 0 || vm.Available > vm.Total || swap.Free > swap.Total {
		return s, errors.New("invalid memory totals")
	}
	s.MemTotal, s.MemUsed = vm.Total, vm.Total-vm.Available
	s.SwapTotal, s.SwapUsed = swap.Total, swap.Total-swap.Free
	s.PressureFree = float64(vm.Available) / float64(vm.Total) * 100
	data, err := os.ReadFile(filepath.Join(p.root, "pressure", "memory"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return s, fmt.Errorf("memory PSI: %w", err)
	}
	s.PsiMemSome10, err = ParsePSI(data)
	if err != nil {
		return s, err
	}
	avg, err := load.AvgWithContext(p.context(ctx))
	if err != nil {
		return s, fmt.Errorf("load average: %w", err)
	}
	s.Load1, s.Load5, s.Load15 = avg.Load1, avg.Load5, avg.Load15
	usage, err := disk.UsageWithContext(ctx, "/")
	if err != nil {
		return s, fmt.Errorf("disk usage: %w", err)
	}
	s.DiskFree = usage.Free
	s.DiskTotal = usage.Total
	if err := machineCPU(ctx, &s); err != nil {
		return s, err
	}
	return s, ctx.Err()
}
