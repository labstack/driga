package platform

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	rmDLL      = windows.NewLazySystemDLL("rstrtmgr.dll")
	rmStart    = rmDLL.NewProc("RmStartSession")
	rmRegister = rmDLL.NewProc("RmRegisterResources")
	rmList     = rmDLL.NewProc("RmGetList")
	rmEnd      = rmDLL.NewProc("RmEndSession")
)

var _ FileProber = winPlatform{}

// OpenFiles checks files below path, including use by this process. Cwd evidence
// is collected separately: Restart Manager does not accept directory resources.
func (winPlatform) OpenFiles(ctx context.Context, path string) (bool, error) {
	return restartManagerFiles(ctx, path, rmFileLimit, rmBatchSize, newRestartManager)
}

type windowsRestartManager uint32

func rmError(op string, code uintptr) error {
	if code != 0 {
		return fmt.Errorf("%s: %w", op, windows.Errno(code))
	}
	return nil
}

func newRestartManager() (restartManager, error) {
	// Resolve every entry before starting a session so missing exports fail closed.
	for _, proc := range []*windows.LazyProc{rmStart, rmRegister, rmList, rmEnd} {
		if err := proc.Find(); err != nil {
			return nil, err
		}
	}
	var session uint32
	var key [33]uint16 // CCH_RM_SESSION_KEY + terminating NUL.
	code, _, _ := rmStart.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0])))
	if err := rmError("RmStartSession", code); err != nil {
		return nil, err
	}
	return windowsRestartManager(session), nil
}

func (s windowsRestartManager) Register(files []string) error {
	names := make([]*uint16, len(files))
	for i, name := range files {
		p, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return err
		}
		names[i] = p
	}
	if len(names) == 0 {
		return nil
	}
	code, _, _ := rmRegister.Call(uintptr(s), uintptr(len(names)), uintptr(unsafe.Pointer(&names[0])), 0, 0, 0, 0)
	runtime.KeepAlive(names)
	return rmError("RmRegisterResources", code)
}

// RM_PROCESS_INFO from restartmanager.h; all fields have DWORD alignment on
// both supported Windows architectures (including the FILETIME pair).
type rmProcessInfo struct {
	PID                                uint32
	Start                              windows.Filetime
	Name                               [256]uint16
	Service                            [64]uint16
	Type, Status, Session, Restartable uint32
}

func (s windowsRestartManager) InUse() (bool, error) {
	var needed, count, reasons uint32
	var apps []rmProcessInfo
	for range 4 {
		var ptr uintptr
		count = uint32(len(apps))
		if len(apps) > 0 {
			ptr = uintptr(unsafe.Pointer(&apps[0]))
		}
		code, _, _ := rmList.Call(uintptr(s), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), ptr, uintptr(unsafe.Pointer(&reasons)))
		runtime.KeepAlive(apps)
		if code == 0 {
			if count == 0 && reasons != 0 {
				return false, fmt.Errorf("RmGetList: unresolved reboot reasons %#x", reasons)
			}
			return count > 0, nil
		}
		if code != uintptr(windows.ERROR_MORE_DATA) {
			return false, rmError("RmGetList", code)
		}
		if needed == 0 || needed > 65536 {
			return false, fmt.Errorf("RmGetList: invalid process count %d", needed)
		}
		apps = make([]rmProcessInfo, needed)
	}
	return false, fmt.Errorf("RmGetList: process list kept changing")
}

func (s windowsRestartManager) End() error {
	code, _, _ := rmEnd.Call(uintptr(s))
	return rmError("RmEndSession", code)
}
