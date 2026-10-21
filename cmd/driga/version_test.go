package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the actual linker symbols, rather than only injecting CLI globals.
func TestVersionStamping(t *testing.T) {
	for _, stamped := range []bool{false, true} {
		name := "driga"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		bin := filepath.Join(t.TempDir(), name)
		args := []string{"build", "-trimpath", "-buildvcs=false", "-o", bin}
		wantVersion, wantCommit, wantDate := "dev", "unknown", "unknown"
		if stamped {
			wantVersion, wantCommit, wantDate = "2026.10.0-alpha", "0123456789abcdef", "2026-10-19T00:00:00Z"
			args = append(args, "-ldflags", "-s -w -X main.version="+wantVersion+" -X main.commit="+wantCommit+" -X main.date="+wantDate)
		}
		build := exec.Command("go", append(args, ".")...)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v: %s", err, out)
		}
		n := newNode(t, bin, "version")
		if out := n.run("version"); !strings.HasPrefix(out, "driga "+wantVersion+"\n") {
			t.Fatalf("unexpected human output: %s", out)
		}
		var result struct {
			Schema int               `json:"schema"`
			Data   map[string]string `json:"data"`
		}
		if err := json.Unmarshal([]byte(n.run("version", "--json")), &result); err != nil || result.Schema != 2 {
			t.Fatalf("invalid JSON: %v", err)
		}
		for key, want := range map[string]string{"version": wantVersion, "commit": wantCommit, "date": wantDate, "go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH} {
			if result.Data[key] != want {
				t.Errorf("%s=%q, want %q", key, result.Data[key], want)
			}
		}
	}
}
