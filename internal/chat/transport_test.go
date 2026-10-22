package chat

import (
	"context"
	"github.com/labstack/driga/internal/fleet"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSocketAndFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	if runtime.GOOS == "windows" {
		t.Skip("Unix socket permissions")
	}
	b, _, _ := fixture(t, true)
	dir := t.TempDir()
	ctx := context.Background()
	closeSocket, err := Listen(ctx, dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSocket()
	info, err := os.Stat(SocketPath(dir))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	c, ephemeral, closeClient, err := Open(ctx, dir, true)
	if err != nil || ephemeral {
		t.Fatal(ephemeral, err)
	}
	defer closeClient()
	row, err := c.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Read(ctx, row.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(ctx, dir, b); err == nil {
		t.Fatal("replaced live broker")
	}
	fallbackDir := filepath.Join(t.TempDir(), "absent")
	c, ephemeral, closeClient, err = Open(ctx, fallbackDir, true)
	if err != nil || !ephemeral {
		t.Fatal(ephemeral, err)
	}
	row, err = c.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Read(ctx, row.ID, 0)
	if err != nil || !s.Ephemeral {
		t.Fatal(s, err)
	}
	closeClient()
	if _, err = os.Stat(filepath.Join(fallbackDir, "driga.db")); !os.IsNotExist(err) {
		t.Fatal("fallback persisted history", err)
	}
}
func TestSocketRefusesRegularFile(t *testing.T) {
	b, _, _ := fixture(t, false)
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	path := SocketPath(dir)
	if err := fleet.EnsureSocketDir(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(context.Background(), dir, b); err == nil {
		t.Fatal("replaced file")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "keep" {
		t.Fatal(string(raw))
	}
}

func TestChatLongDataAndTempPaths(t *testing.T) {
	b, _, _ := fixture(t, false)
	root := t.TempDir()
	longTemp := filepath.Join(root, strings.Repeat("tmp", 50))
	if err := os.MkdirAll(longTemp, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", longTemp)
	t.Setenv("TEMP", longTemp)
	t.Setenv("TMP", longTemp)
	dir := filepath.Join(root, strings.Repeat("data", 40))
	closeSocket, err := Listen(context.Background(), dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSocket()
	c, err := Dial(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if len(SocketPath(dir)) > 100 || filepath.Dir(SocketPath(dir)) != filepath.Dir(fleet.ControlSocketPath(dir)) {
		t.Fatalf("inconsistent socket paths: %s, %s", SocketPath(dir), fleet.ControlSocketPath(dir))
	}
}

func TestChatRefusesUnsafeFallbackDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix ownership and permission contract; Windows uses DACL checks")
	}
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared", true: "symlink"}[symlink], func(t *testing.T) {
			b, _, _ := fixture(t, false)
			root, err := os.MkdirTemp("/tmp", "ds-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			t.Setenv("TMPDIR", root)
			dir := filepath.Join(root, strings.Repeat("data", 40))
			path := SocketPath(dir)
			parent := filepath.Dir(path)
			if symlink {
				target := filepath.Join(root, "target")
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, parent); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(parent, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Listen(context.Background(), dir, b); err == nil {
				t.Fatal("unsafe fallback accepted by listener")
			}
			if _, err := Dial(context.Background(), dir); err == nil {
				t.Fatal("unsafe fallback accepted by client")
			}
		})
	}
}
