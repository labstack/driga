package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unicode/utf16"

	"github.com/labstack/driga/internal/model"
)

// The syscall seams keep Windows layouts and fail-closed collection testable
// on every host. Only platform_windows.go opens handles or dereferences memory.
type winEntry struct {
	PID, PPID int32
	Exe       string
}

// A stale Toolhelp entry can outlive the process, especially while another
// caller holds its handle. Only a confirmed exit may be omitted; query failures
// must retain a partial entry so action identity checks still fail closed.
var errWinProcessGone = errors.New("Windows process exited")

type winDetails struct {
	Start, Kernel, User uint64
	RSS, Private        uint64
	Exe, Cmdline        string
	Argv                []string
	Partial             bool
}
type winMemory struct{ TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, SwapTotal, SwapUsed uint64 }
type winPlatform struct {
	snapshot func() ([]winEntry, error)
	inspect  func(int32) (winDetails, error)
	start    func(int32) (time.Time, error)
	cwd      func(int32) (string, error)
	memory   func() (winMemory, error)
	disk     func() (uint64, uint64, error)
	cores    int
}

func winTime(ticks uint64) time.Time {
	if ticks == 0 {
		return time.Time{}
	}
	return time.Unix(int64(ticks/10000000)-11644473600, int64(ticks%10000000)*100).UTC()
}
func winMachine(m winMemory, cores int) model.MachineSample {
	return model.MachineSample{TS: time.Now(), MemTotal: m.TotalPhys, MemUsed: m.TotalPhys - min(m.AvailPhys, m.TotalPhys),
		CommitLimit: m.TotalPageFile, CommitUsed: m.TotalPageFile - min(m.AvailPageFile, m.TotalPageFile),
		SwapTotal: m.SwapTotal, SwapUsed: min(m.SwapUsed, m.SwapTotal),
		// GlobalMemoryStatusEx reports commit availability, not actual pagefile
		// occupancy. Swap comes only from EnumPageFiles when available.
		// Neither memory load nor commit ratio is an OS pressure level.
		PressureLevel: "unknown", Load1: -1, Load5: -1, Load15: -1, Cores: cores}
}
func (p winPlatform) Procs(ctx context.Context) ([]model.Proc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := p.snapshot()
	if err != nil {
		return nil, err
	}
	ps := make([]model.Proc, 0, len(entries))
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return ps, err
		}
		proc := model.Proc{PID: e.PID, PPID: e.PPID, Exe: e.Exe, Partial: true}
		d, err := p.inspect(e.PID)
		if errors.Is(err, errWinProcessGone) {
			continue
		}
		if err == nil {
			proc.StartTime = winTime(d.Start)
			proc.CPUSeconds = float64(d.Kernel)/1e7 + float64(d.User)/1e7
			proc.RSS, proc.Footprint, proc.MemoryMeasure = d.RSS, d.Private, "private_bytes"
			if d.Exe != "" {
				proc.Exe = d.Exe
			}
			proc.Cmdline, proc.Partial, proc.Argv = d.Cmdline, d.Partial, d.Argv
		}
		ps = append(ps, proc)
	}
	return ps, ctx.Err()
}
func (p winPlatform) Cwds(ctx context.Context, pids []int32) (map[int32]string, error) {
	out := make(map[int32]string)
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if pid > 0 {
			if cwd, err := p.cwd(pid); err == nil && cwd != "" {
				out[pid] = cwd
			}
		}
	}
	return out, ctx.Err()
}
func (p winPlatform) Alive(pid int32, start time.Time) bool {
	if pid <= 0 || start.IsZero() {
		return false
	}
	if p.start != nil {
		created, err := p.start(pid)
		return err == nil && !created.IsZero() && SameStart(created, start)
	}
	d, err := p.inspect(pid)
	return err == nil && !winTime(d.Start).IsZero() && SameStart(winTime(d.Start), start)
}
func (p winPlatform) Machine(ctx context.Context) (model.MachineSample, error) {
	if err := ctx.Err(); err != nil {
		return model.MachineSample{}, err
	}
	m, err := p.memory()
	if err != nil {
		return model.MachineSample{}, err
	}
	s := winMachine(m, p.cores)
	s.DiskFree, s.DiskTotal, err = p.disk()
	if err != nil {
		return s, err
	}
	return s, ctx.Err()
}

func winPointer(b []byte, offset, width int) (uint64, error) {
	if (width != 4 && width != 8) || offset < 0 || offset+width > len(b) {
		return 0, fmt.Errorf("invalid Windows pointer layout")
	}
	if width == 4 {
		return uint64(binary.LittleEndian.Uint32(b[offset:])), nil
	}
	return binary.LittleEndian.Uint64(b[offset:]), nil
}
func winStringDescriptor(b []byte, width int) (uint64, uint32, error) {
	if len(b) < 4 {
		return 0, 0, fmt.Errorf("short UNICODE_STRING")
	}
	n, maxLen := uint32(binary.LittleEndian.Uint16(b)), uint32(binary.LittleEndian.Uint16(b[2:]))
	if n%2 != 0 || n > maxLen {
		return 0, 0, fmt.Errorf("invalid UNICODE_STRING length")
	}
	off := 4
	if width == 8 {
		off = 8
	}
	ptr, err := winPointer(b, off, width)
	if err != nil || n > 0 && ptr == 0 {
		return 0, 0, fmt.Errorf("invalid UNICODE_STRING pointer")
	}
	return ptr, n, nil
}
func winUTF16(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}
func winNTString(b []byte, base uint64, width int) (string, error) {
	ptr, n, err := winStringDescriptor(b, width)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if ptr < base || ptr-base > uint64(len(b)) || uint64(n) > uint64(len(b))-(ptr-base) {
		return "", fmt.Errorf("UNICODE_STRING outside query buffer")
	}
	return winUTF16(b[ptr-base : ptr-base+uint64(n)]), nil
}
func winCwd(peb uint64, width int, read func(uint64, uint32) ([]byte, error)) (string, error) {
	po, co, ps := 0x10, 0x24, 0x28
	if width == 8 {
		po, co, ps = 0x20, 0x38, 0x28
	}
	b, err := read(peb, uint32(ps))
	if err != nil {
		return "", err
	}
	params, err := winPointer(b, po, width)
	if err != nil || params == 0 {
		return "", fmt.Errorf("process parameters unavailable")
	}
	b, err = read(params, uint32(co+2*width))
	if err != nil {
		return "", err
	}
	if len(b) < co {
		return "", fmt.Errorf("short process parameters")
	}
	ptr, n, err := winStringDescriptor(b[co:], width)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("empty current directory")
	}
	b, err = read(ptr, n)
	if err != nil {
		return "", err
	}
	if len(b) != int(n) {
		return "", fmt.Errorf("short current directory read")
	}
	return winUTF16(b), nil
}
