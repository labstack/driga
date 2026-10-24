package attention

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeNotificationCenter struct {
	mu              sync.Mutex
	requests        []NativeNotificationRequest
	authorizations  int
	authErr, addErr error
	gate            <-chan struct{}
	delivered       chan struct{}
}

func (f *fakeNotificationCenter) Authorize(complete func(error)) {
	finish := func() {
		f.mu.Lock()
		f.authorizations++
		err := f.authErr
		f.mu.Unlock()
		complete(err)
	}
	if f.gate != nil {
		go func() { <-f.gate; finish() }()
	} else {
		finish()
	}
}

func (f *fakeNotificationCenter) Add(_ context.Context, n NativeNotificationRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, n)
	if f.delivered != nil {
		f.delivered <- struct{}{}
	}
	return f.addErr
}

func TestMacNativeMappingAndConcurrentAuthorization(t *testing.T) {
	f := &fakeNotificationCenter{}
	m := &MacNotifier{Center: f}
	d := DesktopNotifier{OS: "darwin", Mac: m, Runner: runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		t.Error("notification launched an external helper")
		return nil, nil
	})}
	n := Notification{Kind: "waiting", Title: "Needs you", Body: "Project · permission", Sound: "Glass", ItemID: "item '$(bad) with spaces", MachineID: "local"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := d.Notify(t.Context(), n); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if f.authorizations != 1 || len(f.requests) != 8 {
		t.Fatal(f)
	}
	first := f.requests[0]
	if first.Title != n.Title || first.Body != n.Body || first.Sound != n.Sound || first.ItemID != n.ItemID || first.MachineID != n.MachineID || first.Identifier == "" || first.ThreadIdentifier != first.Identifier {
		t.Fatal(first)
	}
	for _, next := range f.requests {
		if next != first {
			t.Fatal("re-send did not replace", next)
		}
	}
	n.MachineID = "remote"
	if nativeNotificationRequest(n).Identifier == first.Identifier {
		t.Fatal("machines collided")
	}
	n.ItemID = ""
	remote := nativeNotificationRequest(n)
	if remote.ItemID != "" || remote.Identifier == first.Identifier {
		t.Fatal(remote)
	}
	n.MachineID = ""
	if nativeNotificationRequest(n).ItemID != "" {
		t.Fatal("summary has a jump")
	}
}

func TestMacAuthorizationFailureAndCancellation(t *testing.T) {
	denied := errors.New("denied")
	f := &fakeNotificationCenter{authErr: denied}
	m := &MacNotifier{Center: f}
	for range 2 {
		if err := m.Notify(t.Context(), Notification{}); !errors.Is(err, denied) {
			t.Fatal(err)
		}
	}
	// A refusal is re-checked on the next notification, so allowing Driga in
	// System Settings works without restarting the collector.
	if f.authorizations != 2 || len(f.requests) != 0 {
		t.Fatal(f)
	}
	f.authErr = nil
	if err := m.Notify(t.Context(), Notification{}); err != nil || len(f.requests) != 1 {
		t.Fatal("allowing later did not deliver", err, f)
	}
	gate := make(chan struct{})
	f = &fakeNotificationCenter{gate: gate}
	m = &MacNotifier{Center: f}
	ctx, cancel := context.WithCancel(t.Context())
	if err := m.Notify(ctx, Notification{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := m.Notify(ctx, Notification{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(gate)
	<-m.authorized
	if err := m.Notify(t.Context(), Notification{}); err != nil {
		t.Fatal(err)
	}
	if f.authorizations != 1 || len(f.requests) != 1 {
		t.Fatal(f)
	}
	f.addErr = errors.New("delivery failed")
	if err := m.Notify(t.Context(), Notification{}); !errors.Is(err, f.addErr) {
		t.Fatal(err)
	}
}

func TestMacUnconfiguredBackendUnavailable(t *testing.T) {
	for _, d := range []DesktopNotifier{{OS: "darwin"}, {OS: "darwin", Mac: &MacNotifier{}}} {
		if err := d.Notify(t.Context(), Notification{}); err == nil {
			t.Fatal("unbundled notifications available")
		}
	}
}

func TestNativeNotificationClickUsesArgv(t *testing.T) {
	var calls [][]string
	r := runnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	})
	const path, item = "/opt/Driga's app/driga", "item '$(bad) with spaces"
	for _, action := range []string{notificationDefaultAction, "com.apple.UNNotificationDismissActionIdentifier"} {
		for _, id := range []string{item, ""} {
			if err := notificationResponse(t.Context(), r, path, action, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(calls, [][]string{{path, "inbox", "jump", item}}) {
		t.Fatal(calls)
	}
}

func TestDrigaAppExecutable(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"Driga.app/Contents/MacOS/driga", "bin/driga", "Other.app/Contents/MacOS/driga", "Driga.app/Contents/MacOS/other"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if drigaAppExecutable(path) != (rel == "Driga.app/Contents/MacOS/driga") {
			t.Fatal(rel)
		}
	}
}

func TestDecimalBytes(t *testing.T) {
	for n, want := range map[uint64]string{12_154_470_400: "12.2 GB", 20_000_000_000: "20 GB", 512_000_000: "512 MB", 1500: "1.5 kB", 0: "0 B"} {
		if got := DecimalBytes(n); got != want {
			t.Fatalf("%d: %s, want %s", n, got, want)
		}
	}
}

func TestMacPendingAuthorizationQueuesFirstAlert(t *testing.T) {
	gate := make(chan struct{})
	f := &fakeNotificationCenter{gate: gate, delivered: make(chan struct{}, 1)}
	m := &MacNotifier{Center: f}
	n := Notification{Title: "Low disk space", ItemID: "disk:1"}
	// Notify must return while the permission prompt is still pending.
	if err := m.Notify(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	close(gate)
	select {
	case <-f.delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("pending notification lost")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 || f.requests[0].Title != n.Title {
		t.Fatal(f.requests)
	}
}
