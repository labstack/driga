package platform

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOpenFilePathsRefusesUnreadableOwnDescriptors(t *testing.T) {
	if path := os.Getenv("DRIGA_TEST_NONDUMPABLE_FILE"); path != "" {
		f, err := os.Open(path)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if _, _, err := syscall.Syscall6(syscall.SYS_PRCTL, 4, 0, 0, 0, 0, 0); err != 0 {
			panic(err)
		}
		fmt.Println("ready")
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		return
	}
	if os.Getuid() == 0 {
		t.Skip("requires an unprivileged user for procfs permission checks")
	}
	path := filepath.Join(t.TempDir(), "held.bin")
	if err := os.WriteFile(path, []byte("held"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOpenFilePathsRefusesUnreadableOwnDescriptors$")
	cmd.Env = append(os.Environ(), "DRIGA_TEST_NONDUMPABLE_FILE="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close() // The synthetic child exits on EOF; no signals are sent.
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	}()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child readiness: %q %v", line, err)
	}
	if paths, err := New().(OpenFileLister).OpenFilePaths(context.Background()); err == nil {
		t.Fatalf("unreadable own descriptor table treated as complete: %d paths", len(paths))
	}
}

func TestFDPermissionRequiresKnownForeignOwner(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		want         bool
	}{
		{"own", fmt.Sprintf("Uid:\t%d\t%d\t%d\t%d\n", os.Getuid(), os.Getuid(), os.Getuid(), os.Getuid()), false},
		{"foreign", fmt.Sprintf("Uid:\t%d\t%d\t%d\t%d\n", uint32(os.Getuid())+1, uint32(os.Getuid())+1, uint32(os.Getuid())+1, uint32(os.Getuid())+1), true},
		{"unknown", "Name:\tfixture\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "status"), []byte(tc.status), 0600); err != nil {
				t.Fatal(err)
			}
			if got := ignorableFDPermission(dir, os.ErrPermission); got != tc.want {
				t.Fatalf("permission ignored=%t, want %t", got, tc.want)
			}
			if ignorableFDPermission(dir, syscall.EIO) {
				t.Fatal("non-permission inspection failure ignored")
			}
		})
	}
}
