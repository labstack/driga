package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	ks "github.com/kardianos/service"
)

// The helper is exec'd in place of launchctl: no real service manager or user
// registration is reachable, even though the regression crosses New().Status().
func TestLaunchdStatusHelper(t *testing.T) {
	if os.Getenv("DRIGA_TEST_LAUNCHCTL_HELPER") != "1" {
		t.Skip("subprocess fixture")
	}
	if err := os.WriteFile(os.Getenv("DRIGA_TEST_LAUNCHCTL_PID"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(2)
	}
	switch os.Getenv("DRIGA_TEST_LAUNCHCTL_MODE") {
	case "hang":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "missing":
		os.Exit(113)
	case "failure":
		fmt.Fprintln(os.Stderr, "private environment value")
		os.Exit(1)
	case "malformed":
		fmt.Fprintln(os.Stdout, "unexpected")
		os.Exit(0)
	case "stopped":
		fmt.Fprintln(os.Stdout, "{\n\"Label\" = \"fixture\";\n};")
		os.Exit(0)
	case "overflow":
		os.Stdout.Write(bytes.Repeat([]byte("x"), 2<<20))
		os.Exit(0)
	}
	fmt.Fprint(os.Stdout, "{\n\"PID\" = 123;\n")
	os.Stdout.Write(bytes.Repeat([]byte(" "), 256<<10))
	fmt.Fprintln(os.Stdout, "\n};")
	os.Exit(0)
}

func launchdStatusFixture(t *testing.T, mode string) (*launchdController, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "launchctl")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(root, "child.pid")
	t.Setenv("DRIGA_TEST_LAUNCHCTL_HELPER", "1")
	t.Setenv("DRIGA_TEST_LAUNCHCTL_MODE", mode)
	t.Setenv("DRIGA_TEST_LAUNCHCTL_PID", pidPath)
	t.Setenv("DRIGA_TEST_LAUNCHCTL_BINARY", exe)
	t.Setenv("PATH", root)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec \"$DRIGA_TEST_LAUNCHCTL_BINARY\" -test.run=^TestLaunchdStatusHelper$\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := New(filepath.Join(root, "driga"))
	if err != nil {
		t.Fatal(err)
	}
	c.(*launchdController).statusCommand = func(ctx context.Context, args ...string) *exec.Cmd { return exec.CommandContext(ctx, path, args...) }
	return c.(*launchdController), pidPath
}

func TestLaunchdStatusDrainsOutputWithoutBlocking(t *testing.T) {
	c, pidPath := launchdStatusFixture(t, "")
	type result struct {
		state ks.Status
		err   error
	}
	done := make(chan result, 1)
	go func() { state, err := c.Status(); done <- result{state, err} }()
	select {
	case r := <-done:
		if r.err != nil || r.state != ks.StatusRunning {
			t.Fatalf("status=%v error=%v", r.state, r.err)
		}
	case <-time.After(2 * time.Second):
		// Reap only this fixture so the failed regression leaves no processes.
		raw, err := os.ReadFile(pidPath)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			t.Fatal(err)
		}
		_ = p.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("fixture did not stop")
		}
		t.Fatal("service status blocked while a valid launchctl response filled stdout")
	}
}

func TestLaunchdStatusFailuresAndStopped(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		state   ks.Status
		wantErr bool
	}{
		{"stopped", ks.StatusStopped, false},
		{"missing", ks.StatusUnknown, true},
		{"failure", ks.StatusUnknown, true},
		{"malformed", ks.StatusUnknown, true},
		{"overflow", ks.StatusUnknown, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			c, _ := launchdStatusFixture(t, tc.mode)
			state, err := c.Status()
			if state != tc.state || (err != nil) != tc.wantErr {
				t.Fatalf("state=%v err=%v", state, err)
			}
			if err != nil && strings.Contains(err.Error(), "private environment value") {
				t.Fatal("status leaked job output")
			}
			if tc.mode == "missing" && !errors.Is(err, ks.ErrNotInstalled) {
				t.Fatalf("missing job=%v", err)
			}
		})
	}
}

func TestLaunchdStartUsesBoundedStatusQuery(t *testing.T) {
	c, _ := launchdStatusFixture(t, "")
	model := &fakeLaunchd{state: ks.StatusStopped}
	c.Controller = model
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if model.starts != 0 {
		t.Fatal("Start bypassed the running launchctl response")
	}
}

func TestLaunchdStatusTimeoutReapsOwnedQuery(t *testing.T) {
	c, pidPath := launchdStatusFixture(t, "hang")
	started := time.Now()
	state, err := c.Status()
	if state != ks.StatusUnknown || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("state=%v err=%v", state, err)
	}
	if time.Since(started) > launchdStatusTimeout+3*time.Second {
		t.Fatal("status exceeded bounded cancellation")
	}
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("status left its fixture query alive")
	}
}
