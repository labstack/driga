package platform

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRestartManagerOwnedFileHelper(t *testing.T) {
	if os.Getenv("DRIGA_RM_HELPER") != "1" {
		return
	}
	f, err := os.OpenFile(os.Getenv("DRIGA_RM_FILE"), os.O_RDWR, 0600)
	if err != nil {
		os.Exit(2)
	}
	fmt.Println("open")
	_ = bufio.NewScanner(os.Stdin).Scan()
	_ = f.Close()
	os.Exit(0)
}

func TestWindowsOpenFilesOwnedChild(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "owned 子.txt")
	if err := os.WriteFile(name, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRestartManagerOwnedFileHelper$")
	cmd.Env = append(os.Environ(), "DRIGA_RM_HELPER=1", "DRIGA_RM_FILE="+name)
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
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scan := bufio.NewScanner(out)
	if !scan.Scan() || scan.Text() != "open" {
		t.Fatalf("owned file handshake: %q %v", scan.Text(), scan.Err())
	}
	probe := New().(FileProber)
	if open, err := probe.OpenFiles(ctx, dir); err != nil || !open {
		t.Fatalf("child holds file: open=%t err=%v", open, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if open, err := probe.OpenFiles(ctx, dir); err != nil || open {
		t.Fatalf("child closed file: open=%t err=%v", open, err)
	}
	f, err := os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if open, err := probe.OpenFiles(ctx, dir); err != nil || !open {
		t.Fatalf("own handles must count: open=%t err=%v", open, err)
	}
}
