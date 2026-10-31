package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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

// procFromKinfo decodes the identity and state from the existing sysctl sample.
func procFromKinfo(k *unix.KinfoProc) model.Proc {
	const szomb = 5 // XNU sys/proc.h: exited, awaiting parent wait(2).
	return model.Proc{
		PID: k.Proc.P_pid, PPID: k.Eproc.Ppid,
		StartTime: time.UnixMilli(k.Proc.P_starttime.Sec*1000 + int64(k.Proc.P_starttime.Usec)/1000),
		Zombie:    k.Proc.P_stat == szomb,
		OwnerUID:  k.Eproc.Ucred.Uid, OwnerKnown: true,
	}
}

func (d darwinPlatform) Procs(ctx context.Context) ([]model.Proc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// One sysctl yields pid, ppid, start time and command name for every process.
	kprocs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	uid := uint32(os.Getuid())
	ttys := ttyDevices()
	out := make([]model.Proc, 0, len(kprocs))
	seen := make(map[int32]struct{}, len(kprocs))
	for i := range kprocs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		k := &kprocs[i]
		sample := procFromKinfo(k)
		sample.TTY = ttys[uint64(uint32(k.Eproc.Tdev))]
		// Zombies have no task resources or argv to inspect. Keep their state
		// even when the per-process APIs would fail for an exited task.
		if sample.Zombie {
			out = append(out, sample)
			continue
		}
		pid, start := sample.PID, sample.StartTime.UnixMilli()
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
		argv, exe, err := d.idents.get(pid, key, func() ([]string, string, error) {
			argv, err := p.CmdlineSliceWithContext(ctx)
			if err != nil {
				return nil, "", err
			}
			exe, err := p.ExeWithContext(ctx)
			return argv, exe, err
		})
		if err != nil {
			// argv may be unreadable during exec or for other users. Keep the
			// sampled resources for the machine view; expose only the sysctl
			// command name when another user's executable is inaccessible.
			argv, exe = nil, unix.ByteSliceToString(k.Proc.P_comm[:])
			if k.Eproc.Pcred.P_ruid == uid {
				exe = ""
			}
		}
		seen[pid] = struct{}{}
		sample.RSS, sample.CPUSeconds = rss.RSS, times.User+times.System
		sample.Cmdline, sample.Exe, sample.Argv = strings.Join(argv, " "), exe, argv
		sample.MemoryMeasure = "rss"
		if footprint, err := physicalFootprint(pid); err == nil {
			sample.Footprint, sample.MemoryMeasure = footprint, "physical_footprint"
		}
		out = append(out, sample)
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
	// gopsutil does not expose compressed memory. vm_stat reports actual
	// compressor pages and the host page size (including Apple Silicon).
	sample.PressureLevel = "unknown"
	if level, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level"); err == nil {
		sample.PressureLevel = pressureLevelDarwin(level)
	}
	vmCtx, vmCancel := context.WithTimeout(ctx, 2*time.Second)
	if out, err := exec.CommandContext(vmCtx, "vm_stat").Output(); err == nil {
		sample.MemCompressed, _ = ParseCompressed(out)
	}
	vmCancel()
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
	sample.DiskTotal = usage.Total
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
	if err := machineCPU(ctx, &sample); err != nil {
		return sample, err
	}
	return sample, ctx.Err()
}

func (darwinPlatform) Cwds(ctx context.Context, pids []int32) (map[int32]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// proc_pidinfo(PROC_PIDVNODEPATHINFO) is a syscall per pid; lsof needed
	// ~15 s for the 1,700 processes a cleanup check inspects. A process that
	// exited or belongs to another user is simply absent from the result.
	cwds := make(map[int32]string, len(pids))
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return cwds, err
		}
		if pid <= 0 {
			continue
		}
		if cwd, err := (&process.Process{Pid: pid}).CwdWithContext(ctx); err == nil && cwd != "" {
			cwds[pid] = cwd
		}
	}
	return cwds, nil
}

// lsofBatch bounds one lsof argument list; each batch gets lsofBatchDeadline.
// lsofAllDeadline bounds the unrestricted pass that ResolvedPeers adds.
const (
	lsofBatch         = 200
	lsofBatchDeadline = 5 * time.Second
	lsofAllDeadline   = 10 * time.Second
)

// ExternalPeers checks the tree's own Unix and Internet sockets within five
// seconds per batch of 200 pids, so large trees are not cut off on a loaded
// machine.
func (darwinPlatform) ExternalPeers(ctx context.Context, pids []int32) (map[int32]model.PeerState, error) {
	ctx, cancel := lsofTreeDeadline(ctx, pids)
	defer cancel()
	records, err := lsofTree(ctx, pids, "-U", "-i")
	if err != nil {
		return nil, err
	}
	return ParseLsofPeers(records, pids), nil
}

// ResolvedPeers takes the tree's Unix sockets from one lsof pass over all of
// this user's processes, so a listener's clients and the owners of outside
// peers come from the same snapshot as the tree. If that pass fails or is
// incomplete, every pid reports PeerConnected.
func (darwinPlatform) ResolvedPeers(ctx context.Context, pids []int32) (map[int32]model.PeerState, error) {
	treeCtx, cancel := lsofTreeDeadline(ctx, pids)
	defer cancel()
	inet, err := lsofTree(treeCtx, pids, "-i")
	if err != nil {
		return nil, err
	}
	allCtx, cancelAll := context.WithTimeout(ctx, lsofAllDeadline)
	defer cancelAll()
	all, err := exec.CommandContext(allCtx, "lsof", "-U", "-n", "-P", "-F", "pcdnDtT").Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || allCtx.Err() != nil {
		// Exit 1 means lsof skipped something; a missing client must not
		// read as idle.
		result := make(map[int32]model.PeerState, len(pids))
		for _, pid := range pids {
			if pid > 0 {
				result[pid] = model.PeerConnected
			}
		}
		return result, nil
	}
	return ParseLsofPeersAll(append(inet, all...), pids, all), nil
}

func lsofTreeDeadline(ctx context.Context, pids []int32) (context.Context, context.CancelFunc) {
	batches := max(1, (len(pids)+lsofBatch-1)/lsofBatch)
	return context.WithTimeout(ctx, time.Duration(batches)*lsofBatchDeadline)
}

// lsofTree lists the given socket classes of pids. With -a, -U and -i
// intersect instead of forming a union, so each class runs separately while
// keeping the pid restriction.
func lsofTree(ctx context.Context, pids []int32, classes ...string) ([]byte, error) {
	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			ids = append(ids, strconv.FormatInt(int64(pid), 10))
		}
	}
	var records bytes.Buffer
	for offset := 0; offset < len(ids); offset += lsofBatch {
		for _, class := range classes {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// macOS lsof reports a Unix socket's own kernel address in d (some
			// builds use D); t and T identify socket type and TCP state, which
			// -Fpcn alone does not expose.
			out, err := exec.CommandContext(ctx, "lsof", "-a", class, "-n", "-P", "-F", "pcdnDtT", "-p", strings.Join(ids[offset:min(offset+lsofBatch, len(ids))], ",")).Output()
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
	return records.Bytes(), nil
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
