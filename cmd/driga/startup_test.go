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
)

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
