package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHooksInstallUsesUnresolvedExecutable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
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
			cmd := HooksInstallCmd{HooksOptions: HooksOptions{Agent: "all", DryRun: true}}
			if err := cmd.Run(&Globals{Stdin: strings.NewReader(""), Stdout: &out}); err != nil {
				t.Fatal(err)
			}
			if strings.Count(out.String(), want) != 5 {
				t.Fatalf("both installers must use %q for all hooks, got:\n%s", want, out.String())
			}
			for _, path := range []string{filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".codex", "config.toml")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("dry run wrote %s: %v", path, err)
				}
			}
		})
	}
}
