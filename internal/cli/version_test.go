package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestVersionMetadata(t *testing.T) {
	for _, metadata := range [][]string{nil, {"abc123", "2026-10-19T00:00:00Z"}} {
		for _, jsonMode := range []bool{false, true} {
			var out, diagnostic bytes.Buffer
			args := []string{"version"}
			if jsonMode {
				args = append(args, "--json")
			}
			if code := run(args, "dev", nil, &out, &diagnostic, metadata...); code != 0 || diagnostic.Len() != 0 {
				t.Fatalf("code=%d stderr=%s", code, &diagnostic)
			}
			commit, date := "unknown", "unknown"
			if len(metadata) != 0 {
				commit, date = metadata[0], metadata[1]
			}
			if jsonMode {
				var result struct {
					Schema int               `json:"schema"`
					Data   map[string]string `json:"data"`
				}
				if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Schema != 2 {
					t.Fatalf("invalid version envelope: %v: %s", err, &out)
				}
				for key, want := range map[string]string{"version": "dev", "commit": commit, "date": date, "go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH} {
					if result.Data[key] != want {
						t.Errorf("%s=%q, want %q", key, result.Data[key], want)
					}
				}
			} else {
				for _, want := range []string{"driga dev\n", "commit: " + commit, "date: " + date, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
					if !strings.Contains(out.String(), want) {
						t.Errorf("missing %q in %s", want, &out)
					}
				}
			}
		}
	}
}

type versionFailWriter struct{}

func (versionFailWriter) Write([]byte) (int, error) { return 0, errors.New("output closed") }

func TestVersionOutputFailure(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		if err := (VersionCmd{}).Run(&Globals{JSON: jsonMode, Stdout: versionFailWriter{}}); err == nil {
			t.Fatal("output failure was ignored")
		}
	}
}
