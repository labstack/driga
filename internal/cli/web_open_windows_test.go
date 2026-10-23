package cli

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsBrowserLaunch(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	apartment := windows.NewLazySystemDLL("ole32.dll").NewProc("CoGetApartmentType")
	var beforeKind, beforeQualifier uint32
	beforeHR, _, _ := apartment.Call(uintptr(unsafe.Pointer(&beforeKind)), uintptr(unsafe.Pointer(&beforeQualifier)))
	previous := shellOpenBrowser
	t.Cleanup(func() { shellOpenBrowser = previous })
	called := false
	wantErr := errors.New("browser unavailable")
	address := "http://127.0.0.1:7418/?token=test&value=with%20space"
	shellOpenBrowser = func(_ windows.Handle, verb, file, args, cwd *uint16, _ int32) error {
		called = true
		var kind, qualifier uint32
		hr, _, _ := apartment.Call(uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&qualifier)))
		if int32(hr) < 0 || kind != 0 && kind != 3 {
			t.Fatalf("browser handler requires STA: HRESULT=%x apartment=%d", hr, kind)
		}
		if windows.UTF16PtrToString(verb) != "open" || windows.UTF16PtrToString(file) != address || args != nil || cwd != nil {
			t.Fatal("browser URL was changed or interpreted as a shell command")
		}
		return wantErr
	}
	if err := openWebBrowser(context.Background(), address); !called || !errors.Is(err, wantErr) {
		t.Fatalf("browser launch: called=%t err=%v", called, err)
	}
	var afterKind, afterQualifier uint32
	afterHR, _, _ := apartment.Call(uintptr(unsafe.Pointer(&afterKind)), uintptr(unsafe.Pointer(&afterQualifier)))
	if beforeHR != afterHR || int32(afterHR) >= 0 && beforeKind != afterKind {
		t.Fatalf("unbalanced COM initialization: before=%x/%d after=%x/%d", beforeHR, beforeKind, afterHR, afterKind)
	}
	called = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := openWebBrowser(ctx, address); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancelled browser launch: called=%t err=%v", called, err)
	}
}
