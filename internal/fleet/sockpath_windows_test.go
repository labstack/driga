package fleet

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/secfile"
	"golang.org/x/sys/windows"
)

func TestWindowsControlCreatesRunDirectory(t *testing.T) {
	root, err := os.MkdirTemp("", "dc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("LOCALAPPDATA", root)
	dataDir := filepath.Join(root, "data")
	s := &Server{dataDir: dataDir, now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A client arriving first creates the private run directory, but still
	// reports the missing collector socket.
	if _, err := Control(ctx, dataDir, ControlRequest{Op: "status"}); !os.IsNotExist(err) {
		t.Fatal("expected a missing socket before listen", err)
	}
	if err := checkSocketDir(ControlSocketPath(dataDir)); err != nil {
		t.Fatal(err)
	}
	ln, err := s.listenControl(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := checkSocketDir(ControlSocketPath(dataDir)); err != nil {
		t.Fatal(err)
	}
	// A real request covers bind, connect, and the directory ACL check.
	if _, err := Control(ctx, dataDir, ControlRequest{Op: "status"}); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsLongSocketPath(t *testing.T) {
	root, err := os.MkdirTemp("", "ds-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("LOCALAPPDATA", filepath.Join(root, strings.Repeat("long-profile-", 20)))
	// Keep the fallback in test-owned storage as well.
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	for _, kind := range []string{"fleet", "chat"} {
		path := WatchSocketPath(filepath.Join(root, "data"), kind)
		if len(path) >= 108 {
			t.Fatalf("socket path has %d bytes", len(path))
		}
		if path != WatchSocketPath(filepath.Join(root, "data"), kind) {
			t.Fatal("unstable path")
		}
		if path == WatchSocketPath(filepath.Join(root, "other-data"), kind) {
			t.Fatal("data directories share a socket")
		}
		if err := ensureSocketDir(path); err != nil {
			t.Fatal(err)
		}
		if err := checkSocketDir(path); err != nil {
			t.Fatal(err)
		}
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWindowsIdentityDefaultOwner(t *testing.T) {
	// Ordinary creation uses the token's default owner, which can be the
	// Administrators group on elevated CI. The final directory and key must
	// still belong to the individual user and have a private DACL.
	dir := filepath.Join(t.TempDir(), "fleet")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := secfile.CheckPrivate(dir); err != nil {
		t.Fatal(err)
	}
	if err := secfile.CheckPrivate(filepath.Join(dir, "identity.key")); err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateIdentity(dir)
	if err != nil || again.DeviceID != id.DeviceID {
		t.Fatal("identity changed", err)
	}
}

func TestWindowsPrivateSocketDirectory(t *testing.T) {
	root, err := os.MkdirTemp("", "ds-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	sock := filepath.Join(root, "run", "test.sock")
	if err := ensureSocketDir(sock); err != nil {
		t.Fatal(err)
	}
	if err := checkSocketDir(sock); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(filepath.Dir(sock), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = protectWindowsDir(filepath.Dir(sock)) })
	if err := checkSocketDir(sock); err == nil {
		t.Fatal("shared socket directory accepted")
	}
}
