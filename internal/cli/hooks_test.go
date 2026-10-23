package cli

import (
	"bytes"
	"encoding/json"
	"github.com/labstack/driga/internal/config"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHooksOTLPCLI(t *testing.T) {

	g, out := initFixture(t, "y\n")
	cmd := HooksInstallCmd{HooksOptions: HooksOptions{Target: "claude", OTLP: true, Traces: true, DryRun: true}}
	if err := cmd.Run(g); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	if _, e := os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("preview wrote settings", e)
	}
	if !strings.Contains(out.String(), "OTEL_TRACES_EXPORTER") {
		t.Fatal(out.String())
	}
	cmd.DryRun = false
	g.Stdin = strings.NewReader("y\n")
	if err := cmd.Run(g); err != nil {
		t.Fatal(err)
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if v["env"].(map[string]any)["OTEL_LOGS_EXPORTER"] != "otlp" {
		t.Fatal(v)
	}
	if err := cmd.Run(g); err == nil {
		t.Fatal("must refuse existing telemetry")
	}
	g.Stdin = strings.NewReader("y\n")
	if err := (HooksUninstallCmd{HooksOptions: HooksOptions{Target: "claude", OTLP: true}}).Run(g); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if strings.Contains(string(b), "OTEL_") {
		t.Fatal(string(b))
	}
}

func TestHooksInstallUsesUnresolvedExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(bin, "driga")
	if err := os.Symlink(executable, stable); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, name := range []string{"same executable", "different executable", "missing PATH executable"} {
		t.Run(name, func(t *testing.T) {
			want := stable
			if name != "same executable" {
				want = executable
				if err := os.Remove(stable); err != nil {
					t.Fatal(err)
				}
			}
			if name == "different executable" {
				if err := os.WriteFile(stable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			cmd := HooksInstallCmd{HooksOptions: HooksOptions{Target: "all", DryRun: true}}
			if err := cmd.Run(&Globals{Stdin: strings.NewReader(""), Stdout: &out}); err != nil {
				t.Fatal(err)
			}
			if strings.Count(out.String(), want) != 16 {
				t.Fatalf("all installers must use %q for all hooks, got:\n%s", want, out.String())
			}
			for _, path := range []string{filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".codex", "config.toml"), filepath.Join(home, ".copilot", "hooks", "driga.json")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("dry run wrote %s: %v", path, err)
				}
			}
		})
	}
}

func TestHooksConfiguredEndpoint(t *testing.T) {

	g, out := initFixture(t, "y\n")
	if err := os.MkdirAll(filepath.Dir(config.Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Path(), []byte("[intake]\nlisten = \"127.0.0.1:8743\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := (HooksInstallCmd{HooksOptions: HooksOptions{Target: "all", OTLP: true, DryRun: true}}).Run(g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), ":7418") || strings.Count(out.String(), ":8743") != 2 {
		t.Fatal(out.String())
	}
}

type editingPrompt struct {
	path string
	done bool
}

func (r *editingPrompt) Read(b []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	if err := os.WriteFile(r.path, []byte(`{"theme":"concurrent edit"}`), 0600); err != nil {
		return 0, err
	}
	return copy(b, "y\n"), nil
}
func TestHooksCLIRejectsEditAfterPreview(t *testing.T) {

	g, _ := initFixture(t, "")
	path := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"original"}`), 0600); err != nil {
		t.Fatal(err)
	}
	g.Stdin = &editingPrompt{path: path}
	err := (HooksInstallCmd{HooksOptions: HooksOptions{Target: "claude"}}).Run(g)
	if err == nil || !strings.Contains(err.Error(), "settings changed since preview; rerun") {
		t.Fatalf("guard error: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != `{"theme":"concurrent edit"}` {
		t.Fatalf("edit overwritten: %s", b)
	}
	backups, _ := filepath.Glob(path + ".driga-bak-*")
	if len(backups) != 0 {
		t.Fatal(backups)
	}
}
