package desktop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeShell writes a login shell in a fresh directory that prints a fixed
// environment, with noise a profile might print, and records its arguments.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("login shells are a Unix concept")
	}
	dir := t.TempDir()
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

// gatedShell is a login shell that prints PATH=path once the returned gate
// is opened.
func gatedShell(t *testing.T, path string) (open func()) {
	t.Helper()
	dir := fakeShell(t, `while [ ! -f "$(dirname "$0")/gate" ]; do sleep 0.02; done
echo run >> "$(dirname "$0")/runs"
echo "PATH=`+path+`"
`)
	return func() {
		if err := os.WriteFile(filepath.Join(dir, "gate"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeStoredEnv(t *testing.T, dataDir string, env map[string]string) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "app-env.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Every launch reads the login shell again, so a PATH changed since an
// earlier launch reaches the collectors started after the capture ends.
// Until then the stored environment serves.
func TestLoginEnvRefreshesEveryLaunch(t *testing.T) {
	open := gatedShell(t, "/new/bin")
	data := t.TempDir()
	writeStoredEnv(t, data, map[string]string{"PATH": "/old/bin"})
	env := newLoginEnv(data, LoginEnvironment)
	go env.refresh(context.Background())
	got, err := env.get(context.Background())
	if err != nil || !reflect.DeepEqual(got, map[string]string{"PATH": "/old/bin"}) {
		t.Fatalf("before the capture: %v, %v", got, err)
	}
	open()
	<-env.done
	got, err = env.get(context.Background())
	if err != nil || !reflect.DeepEqual(got, map[string]string{"PATH": "/new/bin"}) {
		t.Fatalf("after the capture: %v, %v", got, err)
	}
	stored, err := os.ReadFile(filepath.Join(data, "app-env.json"))
	if err != nil || string(stored) != `{"PATH":"/new/bin"}` {
		t.Fatalf("stored %s, %v", stored, err)
	}
	info, err := os.Stat(filepath.Join(data, "app-env.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("app-env.json: %v, %v", info, err)
	}
}

// The first launch has nothing stored and waits for the capture.
func TestLoginEnvFirstLaunchWaits(t *testing.T) {
	open := gatedShell(t, "/login/bin")
	env := newLoginEnv(t.TempDir(), LoginEnvironment)
	go env.refresh(context.Background())
	done := make(chan map[string]string, 1)
	go func() {
		got, _ := env.get(context.Background())
		done <- got
	}()
	select {
	case got := <-done:
		t.Fatalf("returned %v before the capture", got)
	case <-time.After(50 * time.Millisecond):
	}
	open()
	if got := <-done; !reflect.DeepEqual(got, map[string]string{"PATH": "/login/bin"}) {
		t.Fatalf("environment = %v", got)
	}
}

// A failed capture keeps the stored environment; with none, it is the error.
func TestLoginEnvFailedCapture(t *testing.T) {
	fakeShell(t, "exit 3\n")
	data := t.TempDir()
	env := newLoginEnv(data, LoginEnvironment)
	env.refresh(context.Background())
	if _, err := env.get(context.Background()); err == nil {
		t.Fatal("failed capture without a stored environment accepted")
	}
	writeStoredEnv(t, data, map[string]string{"PATH": "/old/bin"})
	if got, err := env.get(context.Background()); err != nil || got["PATH"] != "/old/bin" {
		t.Fatalf("stored environment: %v, %v", got, err)
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

// The app's own DRIGA_* variables say where it reads and writes, so its
// collector gets exactly those: a login shell's DRIGA_* would point the
// collector at data or settings the app does not show.
func TestChildEnvironmentDrigaFromAppOnly(t *testing.T) {
	for _, entry := range os.Environ() {
		if key, _, _ := strings.Cut(entry, "="); strings.HasPrefix(key, "DRIGA_") {
			t.Setenv(key, "")
			os.Unsetenv(key) // restored by t.Setenv
		}
	}
	t.Setenv("DRIGA_DATA_DIR", "/app/data")
	t.Setenv("DRIGA_CONFIG", "/app/config.toml")
	t.Setenv("DRIGA_APP", "1")
	env := childEnvironment(map[string]string{
		"DRIGA_DATA_DIR": "/login/data", "DRIGA_SPOOL_DIR": "/login/spool", "DRIGA_UI_THEME": "dark",
		"PATH": "/login/bin", "TMUX_TMPDIR": "/tmp/tmux", "XDG_CONFIG_HOME": "/login/xdg",
	})
	driga := []string{}
	for _, entry := range env {
		if strings.HasPrefix(entry, "DRIGA_") {
			driga = append(driga, entry)
		}
	}
	if want := []string{"DRIGA_CONFIG=/app/config.toml", "DRIGA_DATA_DIR=/app/data"}; !slices.Equal(driga, want) {
		t.Fatalf("DRIGA_* = %v, want %v", driga, want)
	}
	for _, want := range []string{"PATH=/login/bin", "TMUX_TMPDIR=/tmp/tmux", "XDG_CONFIG_HOME=/login/xdg"} {
		if !slices.Contains(env, want) {
			t.Fatalf("environment %v lacks %s", env, want)
		}
	}
}

func TestShadowedKeys(t *testing.T) {
	login := map[string]string{"PATH": "/bin", "DRIGA_DATA_DIR": "/shell", "DRIGA_CONFIG": "/same", "DRIGA_SPOOL_DIR": "/spool"}
	own := map[string]string{"DRIGA_CONFIG": "/same", "DRIGA_SPOOL_DIR": "/other"}
	got := shadowedKeys(login, func(k string) string { return own[k] })
	if want := []string{"DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR"}; !slices.Equal(got, want) {
		t.Fatalf("shadowed = %q, want %q", got, want)
	}
}
