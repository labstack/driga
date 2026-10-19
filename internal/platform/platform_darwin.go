package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

type darwinPlatform struct{ idents *identCache }

var _ SocketProber = darwinPlatform{}

// New returns the macOS host collector.
func New() Platform { return darwinPlatform{idents: &identCache{}} }

func (d darwinPlatform) Procs(ctx context.Context) ([]model.Proc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// One sysctl yields pid, ppid, start time and command name for every process.
	kprocs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	out := make([]model.Proc, 0, len(kprocs))
	seen := make(map[int32]struct{}, len(kprocs))
	for i := range kprocs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		k := &kprocs[i]
		pid := k.Proc.P_pid
		start := k.Proc.P_starttime.Sec*1000 + int64(k.Proc.P_starttime.Usec)/1000
		p := &process.Process{Pid: pid}
		rss, err := p.MemoryInfoWithContext(ctx)
		if err != nil {
			continue
		}
		times, err := p.TimesWithContext(ctx)
		if err != nil {
			continue
		}
		key := identKey{start: start, comm: unix.ByteSliceToString(k.Proc.P_comm[:])}
		cmdline, exe, err := d.idents.get(pid, key, func() (string, string, error) {
			cmdline, err := p.CmdlineWithContext(ctx)
			if err != nil {
				return "", "", err
			}
			exe, err := p.ExeWithContext(ctx)
			return cmdline, exe, err
		})
		if err != nil {
			continue
		}
		seen[pid] = struct{}{}
		out = append(out, model.Proc{
			PID: pid, PPID: k.Eproc.Ppid, StartTime: time.UnixMilli(start),
			RSS: rss.RSS, CPUSeconds: times.User + times.System,
			Cmdline: cmdline, Exe: exe,
		})
	}
	d.idents.retain(seen)
	return out, nil
}

func (darwinPlatform) Machine(ctx context.Context) (model.MachineSample, error) {
	sample := model.MachineSample{TS: time.Now()}
	if err := ctx.Err(); err != nil {
		return sample, err
	}
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return sample, fmt.Errorf("virtual memory: %w", err)
	}
	sample.MemUsed, sample.MemTotal = vm.Used, vm.Total
	// gopsutil's Darwin VirtualMemoryStat does not expose compressed memory.
	swap, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return sample, fmt.Errorf("swap memory: %w", err)
	}
	sample.SwapUsed, sample.SwapTotal = swap.Used, swap.Total
	usage, err := disk.UsageWithContext(ctx, "/")
	if err != nil {
		return sample, fmt.Errorf("disk usage: %w", err)
	}
	sample.DiskFree = usage.Free
	avg, err := load.AvgWithContext(ctx)
	if err != nil {
		return sample, fmt.Errorf("load average: %w", err)
	}
	sample.Load1, sample.Load5, sample.Load15 = avg.Load1, avg.Load5, avg.Load15
	sample.Cores, err = cpu.CountsWithContext(ctx, true)
	if err != nil {
		return sample, fmt.Errorf("logical cores: %w", err)
	}
	pressureCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(pressureCtx, "memory_pressure").Output(); err == nil {
		if percent, err := ParsePressure(out); err == nil {
			sample.PressureFree = percent
		}
	}
	return sample, nil
}

func (darwinPlatform) Cwds(ctx context.Context, pids []int32) (map[int32]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cwds := make(map[int32]string)
	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			ids = append(ids, strconv.FormatInt(int64(pid), 10))
		}
	}
	for offset := 0; offset < len(ids); offset += 200 {
		if err := ctx.Err(); err != nil {
			return cwds, err
		}
		end := min(offset+200, len(ids))
		out, err := exec.CommandContext(ctx, "lsof", "-a", "-d", "cwd", "-Fpn", "-p", strings.Join(ids[offset:end], ",")).Output()
		for pid, cwd := range ParseLsofCwd(out) {
			cwds[pid] = cwd
		}
		if ctx.Err() != nil {
			return cwds, ctx.Err()
		}
		var exitErr *exec.ExitError
		if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1) {
			return cwds, fmt.Errorf("lsof cwd: %w", err)
		}
	}
	return cwds, nil
}

// ExternalPeers checks Unix and Internet sockets within a five-second deadline.
func (darwinPlatform) ExternalPeers(ctx context.Context, pids []int32) (map[int32]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			ids = append(ids, strconv.FormatInt(int64(pid), 10))
		}
	}
	var records bytes.Buffer
	for offset := 0; offset < len(ids); offset += 200 {
		// With -a, -U and -i intersect instead of forming a union. Query
		// each socket class separately while retaining the pid restriction.
		for _, class := range []string{"-U", "-i"} {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// D identifies each Unix socket's local address; t and T identify
			// socket type and TCP state, which -Fpcn alone does not expose.
			out, err := exec.CommandContext(ctx, "lsof", "-a", class, "-n", "-P", "-F", "pcnDtT", "-p", strings.Join(ids[offset:min(offset+200, len(ids))], ",")).Output()
			records.Write(out)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var exitErr *exec.ExitError
			if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1) {
				return nil, fmt.Errorf("lsof peers: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ParseLsofPeers(records.Bytes(), pids), nil
}

func (darwinPlatform) Alive(pid int32, start time.Time) bool {
	if pid <= 0 {
		return false
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return false
	}
	ms, err := p.CreateTime()
	if err != nil {
		return false
	}
	return SameStart(time.UnixMilli(ms), start)
}
