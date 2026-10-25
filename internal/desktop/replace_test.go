package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceService(t *testing.T) {
	for _, tc := range []struct {
		goos            string
		installed, done bool
		want            bool
	}{
		{"darwin", true, false, true},
		{"windows", true, false, true},
		{"linux", true, false, false},
		{"darwin", false, false, false},
		{"windows", false, false, false},
		{"darwin", true, true, false},
		{"windows", true, true, false},
		{"freebsd", true, false, false},
	} {
		if got := ReplaceService(tc.goos, tc.installed, tc.done); got != tc.want {
			t.Errorf("ReplaceService(%q, %t, %t) = %t; want %t", tc.goos, tc.installed, tc.done, got, tc.want)
		}
	}
}

type fakeService struct {
	installed    bool
	statusErr    error
	uninstallErr error
	queried      int
	uninstalled  int
}

func (f *fakeService) Installed() (bool, error) {
	f.queried++
	return f.installed, f.statusErr
}

func (f *fakeService) Uninstall() error {
	f.uninstalled++
	return f.uninstallErr
}

func TestReplaceInstalledService(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	svc := &fakeService{installed: true}
	open := func() (installedService, error) { return svc, nil }
	removed, err := replaceService("darwin", dataDir, open)
	if err != nil || !removed || svc.uninstalled != 1 {
		t.Fatalf("first launch: removed=%t err=%v uninstalled=%d", removed, err, svc.uninstalled)
	}
	info, err := os.Stat(filepath.Join(dataDir, replacedMarker))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("marker mode %v; want 0600", info.Mode().Perm())
	}
	// Said once: a service installed again later stays, and is not queried.
	removed, err = replaceService("darwin", dataDir, open)
	if err != nil || removed || svc.uninstalled != 1 || svc.queried != 1 {
		t.Fatalf("second launch: removed=%t err=%v uninstalled=%d queried=%d", removed, err, svc.uninstalled, svc.queried)
	}
}

func TestReplaceServiceLeavesOthersAlone(t *testing.T) {
	failed := errors.New("launchctl failed")
	for name, tc := range map[string]struct {
		goos string
		svc  *fakeService
		err  error
	}{
		"linux":            {"linux", &fakeService{installed: true}, nil},
		"not installed":    {"darwin", &fakeService{}, nil},
		"status failed":    {"windows", &fakeService{statusErr: failed}, failed},
		"uninstall failed": {"darwin", &fakeService{installed: true, uninstallErr: failed}, failed},
	} {
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			removed, err := replaceService(tc.goos, dataDir, func() (installedService, error) { return tc.svc, nil })
			if removed || !errors.Is(err, tc.err) {
				t.Fatalf("removed=%t err=%v; want false, %v", removed, err, tc.err)
			}
			if tc.goos == "linux" && tc.svc.queried != 0 {
				t.Fatal("queried the Linux service")
			}
			if _, err := os.Stat(filepath.Join(dataDir, replacedMarker)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("marker written: %v", err)
			}
		})
	}
}
