package scripts_test

import (
	"bytes"
	"context"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"go.yaml.in/yaml/v3"
)

// Build a tiny program with the release's real linker flags, then inspect its
// PE subsystem. Task Scheduler must not create a console for the service, while
// the interactive CLI must retain its console behavior.
func TestWindowsReleaseExecutableSubsystems(t *testing.T) {
	var config struct {
		Builds []struct {
			ID      string   `yaml:"id"`
			LDFlags []string `yaml:"ldflags"`
		} `yaml:"builds"`
	}
	if err := yaml.Unmarshal([]byte(readRepositoryFile(t, "../.goreleaser.yaml")), &config); err != nil {
		t.Fatal(err)
	}
	want := map[string]uint16{"driga": pe.IMAGE_SUBSYSTEM_WINDOWS_CUI, "drigad": pe.IMAGE_SUBSYSTEM_WINDOWS_GUI, "driga-app": pe.IMAGE_SUBSYSTEM_WINDOWS_GUI}
	for _, build := range config.Builds {
		subsystem, ok := want[build.ID]
		if !ok {
			continue
		}
		delete(want, build.ID)
		t.Run(build.ID, func(t *testing.T) {
			flags, err := template.New("ldflags").Parse(strings.Join(build.LDFlags, " "))
			if err != nil {
				t.Fatal(err)
			}
			var rendered bytes.Buffer
			if err := flags.Execute(&rendered, map[string]string{"Os": "windows", "Version": "dev", "FullCommit": "test", "CommitDate": "test"}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "main.go")
			if err := os.WriteFile(source, []byte("package main\nfunc main() {}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			executable := filepath.Join(dir, "probe.exe")
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-ldflags", rendered.String(), "-o", executable, source)
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=windows", "GOARCH=amd64", "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build release subsystem probe: %v: %s", err, out)
			}
			binary, err := pe.Open(executable)
			if err != nil {
				t.Fatal(err)
			}
			defer binary.Close()
			header, ok := binary.OptionalHeader.(*pe.OptionalHeader64)
			if !ok || header.Subsystem != subsystem {
				t.Fatalf("wrong Windows executable subsystem: %#v; want %d", binary.OptionalHeader, subsystem)
			}
		})
	}
	if len(want) > 0 {
		t.Fatalf("missing Windows release builds: %v", want)
	}
}
