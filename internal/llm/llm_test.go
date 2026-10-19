package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fake(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProbeMissingFlag(t *testing.T) {
	fake(t, `case "$1" in --version) echo '2.1.303';; --help) echo '--safe-mode-extra -p --name';; esac`)
	c, err := Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Require("safe-mode"); err == nil || !strings.Contains(err.Error(), "claude 2.1.303 lacks --safe-mode") {
		t.Fatal(err)
	}
	if err := c.Require("p", "name"); err != nil {
		t.Fatal(err)
	}
}

func TestRunSetsInternalEnvAndName(t *testing.T) {
	dir := fake(t, `case "$1" in --version) echo v1;; --help) echo '-p --name';; *) printf '%s\n' "$DRIGA_INTERNAL" "$@";; esac`)
	t.Setenv("DRIGA_INTERNAL", "0")
	c, err := Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Pin the probed executable even if PATH changes after the probe.
	t.Setenv("PATH", filepath.Join(dir, "missing"))
	out, err := c.Run(context.Background(), []string{"-p"}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "1\n-p\n--name\ndriga-internal\n" {
		t.Fatalf("%q", out)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	fake(t, `case "$1" in --version) echo v1;; --help) echo '-p --name';; *) echo 'failure detail' >&2; exit 7;; esac`)
	c, err := Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run(context.Background(), []string{"-p"}, ""); err == nil || !strings.Contains(err.Error(), "failure detail") || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatal(err)
	}
}

func TestProbeMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Probe(context.Background()); err == nil {
		t.Fatal("missing claude accepted")
	}
}
