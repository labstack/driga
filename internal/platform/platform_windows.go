package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"golang.org/x/sys/windows"
)

var (
	winKernel        = windows.NewLazySystemDLL("kernel32.dll")
	winMemoryStatus  = winKernel.NewProc("GlobalMemoryStatusEx")
	winProcessMemory = winKernel.NewProc("K32GetProcessMemoryInfo")
	winPerformance   = winKernel.NewProc("K32GetPerformanceInfo")
	winPageFiles     = winKernel.NewProc("K32EnumPageFilesW")
)

func New() Platform {
	return winPlatform{snapshot: winSnapshot, inspect: winInspect, start: ProcessStart, cwd: winReadCwd, memory: winReadMemory, disk: winDisk, cores: runtime.NumCPU()}
}

func winSnapshot() ([]winEntry, error) {
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err = windows.Process32First(h, &e); err != nil {
		return nil, err
	}
	var out []winEntry
	for {
		out = append(out, winEntry{int32(e.ProcessID), int32(e.ParentProcessID), windows.UTF16ToString(e.ExeFile[:])})
		err = windows.Process32Next(h, &e)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}
func winTicks(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
func winInspect(pid int32) (winDetails, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) && pid > 0 {
			return winDetails{}, errWinProcessGone
		}
		return winDetails{}, err
	}
	defer windows.CloseHandle(h)
	return winInspectHandle(h)
}

func winInspectHandle(h windows.Handle) (winDetails, error) {
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return winDetails{}, err
	}
	if code != 259 {
		return winDetails{}, errWinProcessGone
	}
	var start, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &start, &exit, &kernel, &user); err != nil {
		return winDetails{}, err
	}
	d := winDetails{Start: winTicks(start), Kernel: winTicks(kernel), User: winTicks(user)}
	var token windows.Token
	if e := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token); e == nil {
		if u, e := token.GetTokenUser(); e == nil {
			d.OwnerSID = u.User.Sid.String()
			d.OwnerKnown = d.OwnerSID != ""
		}
		token.Close()
	}
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	queryErr := windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
	if errors.Is(queryErr, windows.ERROR_INSUFFICIENT_BUFFER) {
		buf = make([]uint16, 32768)
		n = uint32(len(buf))
		queryErr = windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
	}
	if queryErr == nil {
		d.Exe = windows.UTF16ToString(buf[:n])
	} else {
		d.Partial = true
	}
	// PROCESS_MEMORY_COUNTERS_EX uses SIZE_T for every counter after the
	// first two DWORDs. PrivateUsage is private commit, not resident memory.
	var m struct {
		Size, Faults                                                                          uint32
		PeakWS, WS, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile, Private uintptr
	}
	m.Size = uint32(unsafe.Sizeof(m))
	if ok, _, _ := winProcessMemory.Call(uintptr(h), uintptr(unsafe.Pointer(&m)), uintptr(m.Size)); ok != 0 {
		d.RSS, d.Private = uint64(m.WS), uint64(m.Private)
	} else {
		d.Partial = true
	}
	if cmd, err := winCommandLine(h); err == nil {
		d.Cmdline = cmd
		d.Argv, err = windows.DecomposeCommandLine(cmd)
		if err != nil {
			d.Partial = true
		}
	} else {
		d.Partial = true
	}
	return d, nil
}
func winCommandLine(h windows.Handle) (string, error) {
	var needed uint32
	_ = windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, nil, 0, &needed)
	for attempt := 0; attempt < 3; attempt++ {
		if needed < 16 || needed > 1<<20 {
			return "", fmt.Errorf("invalid command line buffer size %d", needed)
		}
		b := make([]byte, needed)
		err := windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, unsafe.Pointer(&b[0]), uint32(len(b)), &needed)
		if err == nil {
			return winNTString(b, uint64(uintptr(unsafe.Pointer(&b[0]))), int(unsafe.Sizeof(uintptr(0))))
		}
		if !errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) && !errors.Is(err, windows.STATUS_BUFFER_TOO_SMALL) {
			return "", err
		}
	}
	return "", fmt.Errorf("command line changed during query")
}
func winReadCwd(pid int32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	width := int(unsafe.Sizeof(uintptr(0)))
	var wow uintptr
	if err := windows.NtQueryInformationProcess(h, windows.ProcessWow64Information, unsafe.Pointer(&wow), uint32(unsafe.Sizeof(wow)), nil); err != nil {
		return "", err
	}
	peb := uint64(wow)
	if wow != 0 {
		width = 4
	} else {
		var basic windows.PROCESS_BASIC_INFORMATION
		if err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&basic), uint32(unsafe.Sizeof(basic)), nil); err != nil {
			return "", err
		}
		peb = uint64(uintptr(unsafe.Pointer(basic.PebBaseAddress)))
	}
	return winCwd(peb, width, func(addr uint64, n uint32) ([]byte, error) {
		if n == 0 || n > 65536 || addr == 0 || uint64(uintptr(addr)) != addr {
			return nil, fmt.Errorf("invalid remote read")
		}
		b := make([]byte, n)
		var read uintptr
		err := windows.ReadProcessMemory(h, uintptr(addr), &b[0], uintptr(n), &read)
		if err != nil {
			return nil, err
		}
		if read != uintptr(n) {
			return nil, fmt.Errorf("short remote read")
		}
		return b, nil
	})
}
func winReadMemory() (winMemory, error) {
	var m struct {
		Length, Load                                                                                  uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtended uint64
	}
	m.Length = uint32(unsafe.Sizeof(m))
	if ok, _, err := winMemoryStatus.Call(uintptr(unsafe.Pointer(&m))); ok == 0 {
		return winMemory{}, err
	}
	var perf struct {
		Size                                                                                                                                    uint32
		CommitTotal, CommitLimit, CommitPeak, PhysicalTotal, PhysicalAvailable, SystemCache, KernelTotal, KernelPaged, KernelNonpaged, PageSize uintptr
		Handles, Processes, Threads                                                                                                             uint32
	}
	perf.Size = uint32(unsafe.Sizeof(perf))
	if ok, _, err := winPerformance.Call(uintptr(unsafe.Pointer(&perf)), uintptr(perf.Size)); ok == 0 {
		return winMemory{}, err
	}
	limit, used := uint64(perf.CommitLimit)*uint64(perf.PageSize), uint64(perf.CommitTotal)*uint64(perf.PageSize)
	swapTotal, swapUsed := winReadPageFiles(uint64(perf.PageSize))
	return winMemory{TotalPhys: m.TotalPhys, AvailPhys: m.AvailPhys, TotalPageFile: limit, AvailPageFile: limit - min(used, limit), SwapTotal: swapTotal, SwapUsed: swapUsed}, nil
}
func winDisk() (uint64, uint64, error) {
	// GetDiskFreeSpaceEx requires an existing directory. Before first collect,
	// use the closest existing ancestor on the data directory's volume.
	dir := config.DataDir()
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0, 0, fmt.Errorf("data volume unavailable")
		}
		dir = parent
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0, err
	}
	var available, total, free uint64
	err = windows.GetDiskFreeSpaceEx(p, &available, &total, &free)
	return available, total, err
}

// Windows callbacks cannot be freed. Register once and serialize sampling so
// the callback uses bounded process-owned state rather than leaked closures.
var winPageState struct {
	sync.Mutex
	total, used uint64
}

type winPageInfo struct {
	Size, Reserved    uint32
	Total, Used, Peak uintptr
}

var winPageCallback = sync.OnceValue(func() uintptr {
	return windows.NewCallback(func(_ uintptr, info *winPageInfo, _ *uint16) uintptr {
		if info != nil {
			winPageState.total += uint64(info.Total)
			winPageState.used += uint64(info.Used)
		}
		return 1
	})
})

func winReadPageFiles(pageSize uint64) (uint64, uint64) {
	if err := winPageFiles.Find(); err != nil {
		return 0, 0
	}
	winPageState.Lock()
	defer winPageState.Unlock()
	winPageState.total, winPageState.used = 0, 0
	if ok, _, _ := winPageFiles.Call(winPageCallback(), 0); ok == 0 {
		return 0, 0
	}
	return winPageState.total * pageSize, winPageState.used * pageSize
}

// InspectProcessHandle reads identity from the very handle retained by the
// snapshot signal path. It never resolves a PID into a different process.
func InspectProcessHandle(h windows.Handle, pid int32) (model.Proc, error) {
	d, e := winInspectHandle(h)
	if e != nil {
		if errors.Is(e, errWinProcessGone) {
			return model.Proc{}, syscall.ESRCH
		}
		return model.Proc{}, e
	}
	return model.Proc{PID: pid, StartTime: winTime(d.Start), Exe: d.Exe, Argv: d.Argv, Cmdline: d.Cmdline, OwnerSID: d.OwnerSID, OwnerKnown: d.OwnerKnown, Partial: d.Partial}, nil
}
