package kill

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"golang.org/x/sys/windows"
)

// Only this test executable is launched, never an agent or an existing process.
func TestWindowsKillHelper(t *testing.T) {
	mode := os.Getenv("DRIGA_KILL_HELPER")
	if mode == "" {
		return
	}
	spawn := func(mode string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsKillHelper$")
		cmd.Env = append(os.Environ(), "DRIGA_KILL_HELPER="+mode)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		return cmd
	}
	if mode == "supervisor" {
		spawn("root")
		os.Exit(0)
	}
	if mode == "leaf" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	child := spawn("leaf")
	fmt.Printf("%d %d\n", os.Getpid(), child.Process.Pid)
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		if scan.Text() == "extra" {
			fmt.Println(spawn("leaf").Process.Pid)
		}
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	os.Exit(0)
}

func windowsChildTree(t *testing.T) (platform.Platform, model.Orphan, []int32) {
	t.Helper()
	dir := t.TempDir()
	for key, value := range map[string]string{"HOME": dir, "USERPROFILE": dir, "APPDATA": dir, "LOCALAPPDATA": dir, "DRIGA_DATA_DIR": filepath.Join(dir, "data"), "DRIGA_SPOOL_DIR": filepath.Join(dir, "spool"), "DRIGA_CONFIG": filepath.Join(dir, "config.toml")} {
		t.Setenv(key, value)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsKillHelper$")
	cmd.Env = append(os.Environ(), "DRIGA_KILL_HELPER=supervisor")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Don't Wait until the IDs arrive: Wait closes the inherited pipe ends.
	scan := bufio.NewScanner(out)
	var handles []windows.Handle
	t.Cleanup(func() {
		for _, h := range handles {
			_ = windows.TerminateProcess(h, 1)
			windows.CloseHandle(h)
		}
		in.Close()
		out.Close()
	})
	read := func() []int32 {
		t.Helper()
		if !scan.Scan() {
			t.Fatalf("owned child handshake: %v", scan.Err())
		}
		var ids []int32
		for _, f := range strings.Fields(scan.Text()) {
			n, err := strconv.ParseInt(f, 10, 32)
			if err != nil || n <= 1 {
				t.Fatalf("invalid owned PID %q", f)
			}
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(n))
			if err != nil {
				t.Fatal(err)
			}
			handles = append(handles, h)
			ids = append(ids, int32(n))
		}
		return ids
	}
	ids := read()
	if len(ids) != 2 {
		t.Fatal(ids)
	}
	if state, err := cmd.Process.Wait(); err != nil || !state.Success() {
		t.Fatalf("supervisor: %v %v", state, err)
	}
	// The supervisor has exited; only the root and leaf remain.
	plat := platform.New()
	ps, err := plat.Procs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	root, ok := derive.NewProcIndex(ps).Get(ids[0])
	if !ok {
		t.Fatal("owned root missing")
	}
	o := model.Orphan{RootPID: root.PID, StartTime: root.StartTime, PIDs: ids, Verified: true, ProcessStarts: make(map[int32]time.Time)}
	for _, p := range ps {
		for _, id := range ids {
			if p.PID == id {
				o.ProcessStarts[id] = p.StartTime
			}
		}
	}
	return plat, o, ids[1:]
}

func TestWindowsKillOwnedOrphanTree(t *testing.T) {
	plat, o, children := windowsChildTree(t)
	bad := o
	bad.StartTime = bad.StartTime.Add(-time.Hour)
	r, err := Tree(context.Background(), plat, bad, children, true, time.Millisecond)
	if err != nil || !r.Aborted || len(r.Signaled) != 0 {
		t.Fatalf("reused PID: %+v %v", r, err)
	}
	r, err = Tree(context.Background(), plat, o, nil, true, time.Millisecond)
	if err != nil || !r.Aborted || len(r.Signaled) != 0 {
		t.Fatalf("changed tree: %+v %v", r, err)
	}
	r, err = Tree(context.Background(), plat, o, children, false, 30*time.Millisecond)
	if err != nil || r.Aborted || len(r.Survivors) != 2 {
		t.Fatalf("graceful: %+v %v", r, err)
	}
	r, err = Tree(context.Background(), plat, o, children, true, 30*time.Millisecond)
	if err != nil || r.Aborted {
		t.Fatalf("force: %+v %v", r, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range o.PIDs {
		for plat.Alive(pid, o.ProcessStarts[pid]) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if plat.Alive(pid, o.ProcessStarts[pid]) {
			t.Fatalf("owned PID %d survived", pid)
		}
	}
}
