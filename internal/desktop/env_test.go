package desktop

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

// fakeShell writes a login shell under the isolated HOME that prints a fixed
// environment, with noise a profile might print, and records its arguments.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("login shells are a Unix concept")
	}
	dir := filepath.Join(os.Getenv("HOME"), t.Name())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", path)
	return dir
}

func TestLoginEnvironmentKeepsServiceKeys(t *testing.T) {
	dir := fakeShell(t, `echo "$@" > "$(dirname "$0")/args"
echo "Welcome to the profile"
echo "PATH=/opt/homebrew/bin:/usr/bin"
echo "TMUX_TMPDIR=/tmp/tmux"
echo "DRIGA_DATA_DIR=/data/driga"
echo "SECRET_TOKEN=hunter2"
echo "PWD=/somewhere"
echo "not a variable"
`)
	got, err := LoginEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin", "TMUX_TMPDIR": "/tmp/tmux", "DRIGA_DATA_DIR": "/data/driga"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "-l -c env\n" {
		t.Fatalf("shell args = %q", args)
	}
}

func TestLoginEnvironmentFailingShell(t *testing.T) {
	fakeShell(t, "exit 3\n")
	if _, err := LoginEnvironment(context.Background()); err == nil {
		t.Fatal("failing shell accepted")
	}
}

func TestLoadEnvironmentCapturesOnce(t *testing.T) {
	dir := fakeShell(t, `echo run >> "$(dirname "$0")/runs"
echo "PATH=/login/bin"
`)
	data := t.TempDir()
	for range 2 {
		got, err := LoadEnvironment(context.Background(), data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, map[string]string{"PATH": "/login/bin"}) {
			t.Fatalf("environment = %v", got)
		}
	}
	runs, err := os.ReadFile(filepath.Join(dir, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if string(runs) != "run\n" {
		t.Fatalf("shell ran %q, want once", runs)
	}
	info, err := os.Stat(filepath.Join(data, "app-env.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("app-env.json mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestChildEnvironmentMergesLoginOverBase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows children inherit the session environment")
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("SECRET_TOKEN", "hunter2")
	t.Setenv("LANG", "en_US.UTF-8")
	env := childEnvironment(map[string]string{"PATH": "/login/bin", "TMUX_TMPDIR": "/tmp/tmux"})
	for _, want := range []string{"HOME=" + os.Getenv("HOME"), "LANG=en_US.UTF-8", "PATH=/login/bin", "TMUX_TMPDIR=/tmp/tmux"} {
		if !slices.Contains(env, want) {
			t.Fatalf("environment %v lacks %s", env, want)
		}
	}
	for _, entry := range env {
		if entry == "SECRET_TOKEN=hunter2" || entry == "PATH=/usr/bin:/bin" {
			t.Fatalf("environment %v carries %s", env, entry)
		}
	}
	if !slices.IsSorted(env) {
		t.Fatalf("environment not sorted: %v", env)
	}
}
