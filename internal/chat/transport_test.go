package chat

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSocketAndFallback(t *testing.T) {
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
	path := SocketPath(dir)
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
