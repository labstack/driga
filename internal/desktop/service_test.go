package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/labstack/driga/internal/collector"
)

// The service manager knows one service per account, so an app run with
// another HOME must leave it alone; the same home through a symlink is the
// same home.
func TestIsolatedHome(t *testing.T) {
	if !isolatedHome(func() (string, string) { return "/tmp/fake-home", "/Users/someone" }) {
		t.Fatal("another HOME is not isolated")
	}
	if isolatedHome(func() (string, string) { return "", "/Users/someone" }) {
		t.Fatal("an unset HOME is isolated")
	}
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	if isolatedHome(func() (string, string) { return link, real }) {
		t.Fatal("a symlinked home is isolated")
	}
}

func TestServiceStartupUsesExistingRegistration(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, exists := range []bool{false, true} {
			name := goos + map[bool]string{false: " absent", true: " installed"}[exists]
			t.Run(name, func(t *testing.T) {
				var calls []string
				err := startService(goos, "v2", func() (bool, error) { calls = append(calls, "probe"); return exists, nil }, func(action string) error { calls = append(calls, action); return nil }, func() (collector.Owner, bool, error) {
					calls = append(calls, "owner")
					return collector.Owner{Version: "v2"}, true, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"install", "owner"}
				if goos != "darwin" {
					action := "install"
					if exists {
						action = "start"
					}
					want = []string{"probe", action, "owner"}
				}
				if !reflect.DeepEqual(calls, want) {
					t.Fatalf("startup calls %v, want %v", calls, want)
				}
			})
		}
	}
}

func TestServiceStartupStillRestartsDifferentVersion(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			var calls []string
			err := startService(goos, "v2", func() (bool, error) { return true, nil }, func(action string) error { calls = append(calls, action); return nil }, func() (collector.Owner, bool, error) { return collector.Owner{Version: "v1"}, true, nil })
			if err != nil {
				t.Fatal(err)
			}
			first := "start"
			if goos == "darwin" {
				first = "install"
			}
			if want := []string{first, "restart"}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("version drift calls %v, want %v", calls, want)
			}
		})
	}
}

func TestServiceStartupFailuresStopLaterActions(t *testing.T) {
	failure := errors.New("injected service failure")
	for _, stage := range []string{"probe", "start", "owner", "restart"} {
		t.Run(stage, func(t *testing.T) {
			var calls []string
			err := startService("linux", "v2", func() (bool, error) {
				calls = append(calls, "probe")
				if stage == "probe" {
					return false, failure
				}
				return true, nil
			}, func(action string) error {
				calls = append(calls, action)
				if action == stage {
					return failure
				}
				return nil
			}, func() (collector.Owner, bool, error) {
				calls = append(calls, "owner")
				if stage == "owner" {
					return collector.Owner{}, false, failure
				}
				return collector.Owner{Version: "v1"}, true, nil
			})
			if !errors.Is(err, failure) {
				t.Fatalf("lost startup failure: %v", err)
			}
			all := []string{"probe", "start", "owner", "restart"}
			stop := 0
			for all[stop] != stage {
				stop++
			}
			if !reflect.DeepEqual(calls, all[:stop+1]) {
				t.Fatalf("startup continued after %s failure: %v", stage, calls)
			}
		})
	}
}
