package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/service"
)

func TestDaemonMalformedConfigWritesServiceLog(t *testing.T) {
	name := "drigad"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	root := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "CODEX_HOME", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	t.Setenv("DRIGA_CONFIG", filepath.Join(root, "config.toml"))
	t.Setenv("DRIGA_APP", "")
	if err := os.Unsetenv("DRIGA_APP"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("COPILOT_HOME", "")
	if err := os.WriteFile(os.Getenv("DRIGA_CONFIG"), []byte("capture_text = [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, bin)
	configureStartupCommand(cmd)
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() == 0 || !strings.Contains(string(out), "toml: array is incomplete") {
		t.Fatalf("malformed config exit: %v: %s", err, out)
	}
	log, err := os.ReadFile(service.LogPath())
	if err != nil || !strings.Contains(string(log), "toml: array is incomplete") {
		t.Fatalf("startup failure missing from service log: %v: %s", err, log)
	}
}

// Exercise Main, including path resolution, with a genuinely missing default
// config. Other binary fixtures create an explicit config before startup.
func TestFreshInstallConfig(t *testing.T) {
	name := "driga"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-buildvcs=false", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}

	root := t.TempDir()
	var env []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "DRIGA_") {
			env = append(env, pair)
		}
	}
	env = append(env,
		"HOME="+root, "USERPROFILE="+root,
		"APPDATA="+filepath.Join(root, "AppData", "Roaming"),
		"LOCALAPPDATA="+filepath.Join(root, "AppData", "Local"),
		"XDG_CONFIG_HOME="+filepath.Join(root, ".config"),
	)
	run := func(override string, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		configureStartupCommand(cmd)
		cmd.Dir = root
		cmd.Env = append([]string(nil), env...)
		if override != "" {
			cmd.Env = append(cmd.Env, "DRIGA_CONFIG="+override)
		}
		return cmd.CombinedOutput()
	}

	t.Run("missing default uses defaults", func(t *testing.T) {
		path, err := run("", "config", "path")
		if err != nil {
			t.Fatalf("config path: %v: %s", err, path)
		}
		configPath := strings.TrimSpace(string(path))
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Fatalf("expected absent default config %q, got %v", configPath, err)
		}
		out, err := run("", "config", "show")
		if err != nil || !strings.Contains(string(out), "capture_text = ") || !strings.Contains(string(out), "titles") {
			t.Fatalf("fresh install config show: %v: %s", err, out)
		}
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Fatalf("reading defaults created config %q: %v", configPath, err)
		}
	})

	t.Run("missing explicit config remains an error", func(t *testing.T) {
		out, err := run("missing.toml", "config", "show")
		if err == nil || !strings.Contains(string(out), "load DRIGA_CONFIG:") {
			t.Fatalf("missing explicit config: %v: %s", err, out)
		}
	})

	t.Run("relative explicit config is loaded", func(t *testing.T) {
		const name = "custom config.toml"
		if err := os.WriteFile(filepath.Join(root, name), []byte("capture_text = 'snippets'\n"), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := run(name, "config", "show")
		if err != nil || !strings.Contains(string(out), "capture_text = ") || !strings.Contains(string(out), "snippets") {
			t.Fatalf("explicit config: %v: %s", err, out)
		}
	})
}
