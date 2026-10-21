package platform

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsJobHelper(t *testing.T) {
	mode := os.Getenv("DRIGA_JOB_HELPER")
	if mode == "" {
		return
	}
	if mode == "leaf" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsJobHelper$")
	cmd.Env = append(os.Environ(), "DRIGA_JOB_HELPER=leaf")
	if err := cmd.Start(); err != nil {
		os.Exit(3)
	}
	fmt.Println(cmd.Process.Pid)
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestWindowsJobEndsOwnedDescendants(t *testing.T) {
	dir := t.TempDir()
	for key, value := range map[string]string{"HOME": dir, "USERPROFILE": dir, "APPDATA": dir, "LOCALAPPDATA": dir, "DRIGA_DATA_DIR": filepath.Join(dir, "data"), "DRIGA_SPOOL_DIR": filepath.Join(dir, "spool"), "DRIGA_CONFIG": filepath.Join(dir, "config.toml")} {
		t.Setenv(key, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsJobHelper$")
	cmd.Env = append(os.Environ(), "DRIGA_JOB_HELPER=root")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stop, err := StartChild(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	scan := bufio.NewScanner(out)
	if !scan.Scan() {
		t.Fatalf("job child handshake: %v", scan.Err())
	}
	pid, err := strconv.ParseUint(scan.Text(), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	stop()
	_ = cmd.Wait()
	if status, err := windows.WaitForSingleObject(h, 5000); err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("owned grandchild survived job closure: %d %v", status, err)
	}
}
