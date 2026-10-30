package chat

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/secfile"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	// Listen protects the data directory before checking its socket; do the
	// same so the fixture matches a real data directory on every OS.
	if err := secfile.MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	if err := secfile.Protect(dir); err != nil {
		t.Fatal(err)
	}
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

// A loaded collector can take longer than the hook's old 200 ms health
// budget even though it is listening and able to register the permission.
func TestHookRegistersWithSlowBroker(t *testing.T) {
	b, _, _ := fixture(t, true)
	dir := t.TempDir()
	if err := secfile.MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	if err := secfile.Protect(dir); err != nil {
		t.Fatal(err)
	}
	path := SocketPath(dir)
	if err := fleet.EnsureSocketDir(path); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	b.clientToken = "test-hook-token"
	if err := os.WriteFile(filepath.Join(dir, "chat.key"), []byte(b.clientToken), 0600); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	h := handler(b)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			// Never finish a preflight before its deadline. No timing race or
			// platform-specific scheduling is needed to reproduce the lost prompt.
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		if r.URL.Path == "/hook-prompt" {
			select {
			case <-time.After(300 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		h.ServeHTTP(w, r)
	})}
	defer server.Close()
	go server.Serve(l)
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 200*time.Millisecond)
	c, err := DialHook(dialCtx, dir)
	cancelDial()
	if err != nil {
		t.Fatalf("hook did not register prompt: %v", err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		out, err := c.WaitPrompt(ctx, testPrompt("slow"))
		if err == nil && out.Behavior != "allow" {
			err = fmt.Errorf("decision: %+v", out)
		}
		done <- err
	}()
	waitPrompt(t, b, "slow")
	if err := b.AnswerPrompt(ctx, "slow", true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
