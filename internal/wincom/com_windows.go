// Package wincom contains the small, synchronous COM/WinRT ABI used by Driga.
// All calls and releases must stay on the initialized OS thread. No callbacks
// cross into Go, and no native object retains a pointer into the Go heap.
package wincom

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var combase = windows.NewLazySystemDLL("combase.dll")
var ole32 = windows.NewLazySystemDLL("ole32.dll")
var oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")

var procRoInitialize = combase.NewProc("RoInitialize")
var procRoUninitialize = combase.NewProc("RoUninitialize")
var procCoInitializeEx = ole32.NewProc("CoInitializeEx")
var procCoUninitialize = ole32.NewProc("CoUninitialize")
var procCoInitializeSecurity = ole32.NewProc("CoInitializeSecurity")
var procCoCreateInstance = ole32.NewProc("CoCreateInstance")
var procWindowsCreateString = combase.NewProc("WindowsCreateString")
var procWindowsDeleteString = combase.NewProc("WindowsDeleteString")
var procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
var procRoActivateInstance = combase.NewProc("RoActivateInstance")
var procSysAllocString = oleaut32.NewProc("SysAllocString")
var procSysFreeString = oleaut32.NewProc("SysFreeString")

type HRESULT uint32

func (h HRESULT) Error() string { return fmt.Sprintf("COM HRESULT 0x%08x", uint32(h)) }
func Check(h uintptr) error {
	if int32(h) < 0 {
		return HRESULT(h)
	}
	return nil
}

// Initialize balances even S_FALSE with RoUninitialize, on the same thread.
func Initialize() (func(), error) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, fmt.Errorf("native COM backend requires Windows amd64 or arm64")
	}
	runtime.LockOSThread()
	p := procRoInitialize
	if err := p.Find(); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	h, _, _ := p.Call(1) // RO_INIT_MULTITHREADED
	if err := Check(h); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	return func() { procRoUninitialize.Call(); runtime.UnlockOSThread() }, nil
}

// InitializeSTA is required by the Shell's IFileOperation interface. It must
// not share the MTA initialization used by Task Scheduler and WinRT toasts.
func InitializeSTA() (func(), error) {
	runtime.LockOSThread()
	h, _, _ := procCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED
	if err := Check(h); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	return func() { procCoUninitialize.Call(); runtime.UnlockOSThread() }, nil
}

var securityOnce sync.Once
var securityError error

// Security enables local Task Scheduler impersonation, following Microsoft's
// Task Scheduler COM examples. An embedding host may already own COM security.
func Security() error {
	securityOnce.Do(func() {
		h, _, _ := procCoInitializeSecurity.Call(0, ^uintptr(0), 0, 0, 6, 3, 0, 0, 0)
		if uint32(h) != 0x80010119 {
			securityError = Check(h)
		} // RPC_E_TOO_LATE
	})
	return securityError
}

type Object struct{ vtable *[32]uintptr }

//go:uintptrescapes
func (o *Object) Call(slot int, args ...uintptr) error {
	if o == nil {
		return fmt.Errorf("nil COM object")
	}
	h, _, _ := syscall.SyscallN(o.vtable[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	runtime.KeepAlive(o)
	return Check(h)
}
func (o *Object) Release() {
	if o != nil {
		_ = o.Call(2)
	}
}
func (o *Object) Query(iid string) (*Object, error) {
	id, err := windows.GUIDFromString(iid)
	if err != nil {
		return nil, err
	}
	var out *Object
	err = o.Call(0, uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&out)))
	return out, err
}
func Create(class, iid string) (*Object, error) {
	cls, err := windows.GUIDFromString(class)
	if err != nil {
		return nil, err
	}
	id, err := windows.GUIDFromString(iid)
	if err != nil {
		return nil, err
	}
	var out *Object
	h, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&cls)), 0, 1, uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&out)))
	return out, Check(h)
}

type HString uintptr

func String(s string) (HString, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var out HString
	h, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&out)))
	runtime.KeepAlive(u)
	return out, Check(h)
}
func (s HString) Close() {
	if s != 0 {
		procWindowsDeleteString.Call(uintptr(s))
	}
}
func Factory(class, iid string) (*Object, error) {
	s, err := String(class)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	id, err := windows.GUIDFromString(iid)
	if err != nil {
		return nil, err
	}
	var out *Object
	h, _, _ := procRoGetActivationFactory.Call(uintptr(s), uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&out)))
	return out, Check(h)
}
func Activate(class string) (*Object, error) {
	s, err := String(class)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var out *Object
	h, _, _ := procRoActivateInstance.Call(uintptr(s), uintptr(unsafe.Pointer(&out)))
	return out, Check(h)
}
func BString(s string) (uintptr, func(), error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, nil, err
	}
	b, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(&u[0])))
	runtime.KeepAlive(u)
	if b == 0 {
		return 0, nil, fmt.Errorf("allocate COM string")
	}
	return b, func() { procSysFreeString.Call(b) }, nil
}
