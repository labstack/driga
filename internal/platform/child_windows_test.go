package platform

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsJobHelper(t *testing.T) {
	mode := os.Getenv("DRIGA_JOB_HELPER")
	if mode == "" {
		return
	}
	if mode == "console" {
		console, _, _ := winKernel.NewProc("GetConsoleWindow").Call()
		fmt.Printf("console=%t\n", console != 0)
		os.Exit(0)
	}
	if mode == "console-parent" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsJobHelper$")
		cmd.Env = append(os.Environ(), "DRIGA_JOB_HELPER=console")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := ChildOutput(cmd, false)
		if err != nil {
			fmt.Println(err)
			os.Exit(4)
		}
		fmt.Print(string(out))
		os.Exit(0)
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

func TestWindowsBackgroundChildDoesNotInheritConsole(t *testing.T) {
	t.Run("windowless-parent", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsJobHelper$")
		cmd.Env = append(os.Environ(), "DRIGA_JOB_HELPER=console-parent")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("console-less parent: %v: %s", err, out)
		}
		if got := strings.TrimSpace(string(out)); got != "console=false" {
			t.Fatalf("background child has a console: %s", got)
		}
	})
	for _, combined := range []bool{false, true} {
		t.Run(fmt.Sprintf("combined=%t", combined), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsJobHelper$")
			cmd.Env = append(os.Environ(), "DRIGA_JOB_HELPER=console")
			// HideWindow alone leaves console inheritance in place. Background
			// children must have no console instead.
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			out, err := ChildOutput(cmd, combined)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(out)); got != "console=false" {
				t.Fatalf("background child has a console: %s", got)
			}
			if !cmd.SysProcAttr.HideWindow {
				t.Fatal("caller process attributes were lost")
			}
		})
	}
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
