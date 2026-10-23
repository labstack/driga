package cli

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsBrowserLaunch(t *testing.T) {
	previous := shellOpenBrowser
	t.Cleanup(func() { shellOpenBrowser = previous })
	called := false
	wantErr := errors.New("browser unavailable")
	address := "http://127.0.0.1:7418/?token=test&value=with%20space"
	shellOpenBrowser = func(_ windows.Handle, verb, file, args, cwd *uint16, _ int32) error {
		called = true
		if windows.UTF16PtrToString(verb) != "open" || windows.UTF16PtrToString(file) != address || args != nil || cwd != nil {
			t.Fatal("browser URL was changed or interpreted as a shell command")
		}
		return wantErr
	}
	if err := openWebBrowser(context.Background(), address); !called || !errors.Is(err, wantErr) {
		t.Fatalf("browser launch: called=%t err=%v", called, err)
	}
	called = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := openWebBrowser(ctx, address); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancelled browser launch: called=%t err=%v", called, err)
	}
}
