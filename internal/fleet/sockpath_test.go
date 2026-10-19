package fleet

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// macOS limits Unix socket paths to 104 bytes (Linux: 108); a long data dir
// must not stop serving.
func TestControlSocketPathFitsLimit(t *testing.T) {
	short := "/srv/d"
	if got := ControlSocketPath(short); got != filepath.Join(short, "fleet", "fleet.sock") {
		t.Fatalf("short data dir should keep its socket: %s", got)
	}
	long := filepath.Join(t.TempDir(), strings.Repeat("x", 120))
	got := ControlSocketPath(long)
	if len(got) > maxSocketPath || got != ControlSocketPath(long) {
		t.Fatalf("long data dir socket %q (len %d) must be stable and <= %d", got, len(got), maxSocketPath)
	}
	if ControlSocketPath(long+"y") == got {
		t.Fatal("different data dirs must not share a socket")
	}
}

func TestControlListensWithLongDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.MkdirAll(filepath.Join(dir, "fleet"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Server{dataDir: dir}
	ln, err := s.listenControl(context.Background())
	if err != nil {
		t.Fatalf("control socket with long data dir: %v", err)
	}
	ln.Close()
	if info, err := os.Stat(filepath.Dir(ControlSocketPath(dir))); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("socket dir must be 0700: %v %v", info, err)
	}
}

// Enroll must reach the collector through the same socket path the server
// uses, including the short fallback for long data dirs.
func TestEnrollReachesControlSocketWithLongDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("e", 120))
	sock := ControlSocketPath(dir)
	if err := ensureSocketDir(sock); err != nil {
		t.Fatal(err)
	}
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		bufio.NewReader(c).ReadBytes('\n')
		json.NewEncoder(c).Encode(ControlResponse{Error: "reached collector"})
	}()
	_, err = Enroll(context.Background(), dir, EnrollOpts{Window: time.Minute}, func(string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "reached collector") {
		t.Fatalf("enroll did not reach the control socket: %v", err)
	}
}

func TestControlSocketShortWithLongTMPDIR(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), strings.Repeat("tmp", 50)))
	for _, runtime := range []string{"", "/tmp", strings.Repeat("/runtime", 30)} {
		t.Setenv("XDG_RUNTIME_DIR", runtime)
		got := ControlSocketPath(strings.Repeat("/data", 30))
		if len(got) > maxSocketPath || !strings.HasPrefix(got, "/tmp/") {
			t.Fatalf("fallback %q is not short", got)
		}
		if runtime == "/tmp" && filepath.Dir(got) != "/tmp/driga" {
			t.Fatalf("runtime dir not preferred: %s", got)
		}
	}
}

func TestControlRefusesForeignOwnedDir(t *testing.T) {
	testUnsafeControlDir(t, false)
}
func TestControlRefusesSymlinkedDir(t *testing.T) {
	testUnsafeControlDir(t, true)
}
func testUnsafeControlDir(t *testing.T, symlink bool) {
	t.Helper()
	// chmod simulates an untrusted/shared directory without requiring chown.
	base, err := os.MkdirTemp("/tmp", "driga-unsafe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	dir := filepath.Join(base, "fleet")
	target := dir
	if symlink {
		target = filepath.Join(base, "target")
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if symlink {
		if err := os.Symlink(target, dir); err != nil {
			t.Fatal(err)
		}
	}
	sock := filepath.Join(dir, "fleet.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !symlink {
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			json.NewEncoder(c).Encode(ControlResponse{OK: true})
			c.Close()
		}
	}()
	for _, enroll := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var err error
		if enroll {
			_, err = Enroll(ctx, base, EnrollOpts{AllowID: "pin"}, nil)
		} else {
			_, err = Control(ctx, base, ControlRequest{Op: "status"})
		}
		cancel()
		if err == nil || !strings.Contains(err.Error(), "fleet control dir") {
			t.Errorf("enroll=%v unsafe directory accepted: %v", enroll, err)
		}
	}
}
