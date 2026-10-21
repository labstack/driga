package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/pathx"
)

func TestWindowsOwnedChild(t *testing.T) {
	if os.Getenv("DRIGA_WINDOWS_CHILD") == "1" {
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		return
	}
	dir := filepath.Join(t.TempDir(), "known cwd 子")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsOwnedChild$", "--", "known argument 子")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DRIGA_WINDOWS_CHILD=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		ps, err := p.Procs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, proc := range ps {
			if proc.PID != int32(cmd.Process.Pid) {
				continue
			}
			if proc.PPID != int32(os.Getpid()) || proc.StartTime.IsZero() || !strings.Contains(proc.Cmdline, "known argument 子") || proc.Exe == "" || proc.Partial {
				t.Fatalf("incomplete own child: %+v", proc)
			}
			cwds, err := p.Cwds(ctx, []int32{proc.PID})
			if err != nil || !pathx.Equal(cwds[proc.PID], dir) {
				t.Fatalf("cwd=%q want=%q err=%v", cwds[proc.PID], dir, err)
			}
			if !p.Alive(proc.PID, proc.StartTime) || p.Alive(proc.PID, proc.StartTime.Add(2*time.Minute)) {
				t.Fatal("identity liveness mismatch")
			}
			return
		}
		if ctx.Err() != nil {
			t.Fatal("child absent")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWindowsMachineSyscalls(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(root, "new", "data"))
	s, err := New().Machine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.MemTotal == 0 || s.MemUsed > s.MemTotal || s.CommitLimit == 0 || s.CommitUsed > s.CommitLimit || s.DiskFree == 0 || s.DiskFree > s.DiskTotal || s.Cores < 1 || s.Load1 != -1 || s.PressureLevel != "unknown" {
		t.Fatalf("%+v", s)
	}
}
