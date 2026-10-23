package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type panicHookReader struct{}

func (panicHookReader) Read([]byte) (int, error) { panic("fixture reader panic") }
func TestHookPanicsAndUnwritableSpoolExitZero(t *testing.T) {
	for _, adapter := range []string{"claude", "codex", "copilot"} {
		t.Run(adapter, func(t *testing.T) {
			dir := hookTestEnv(t)
			var out, stderr bytes.Buffer
			event := "SessionStart"
			if adapter == "copilot" {
				event = "sessionStart"
			}
			if code := run([]string{"hook", adapter, event}, "test", panicHookReader{}, &out, &stderr); code != 0 || out.Len() != 0 {
				t.Fatal(code, out.String())
			}
			log, err := os.ReadFile(filepath.Join(dir, "hook-errors.log"))
			if err != nil || !bytes.Contains(log, []byte("panic")) {
				t.Fatal(string(log), err)
			}
			if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(dir, "file", "spool"))
			payload := `{"session_id":"s","sessionId":"s","timestamp":1,"cwd":"/repo"}`
			if code := run([]string{"hook", adapter, event}, "test", strings.NewReader(payload), &out, &stderr); code != 0 || out.Len() != 0 {
				t.Fatal(code, out.String())
			}
		})
	}
}
func TestHookRecoversParentPanic(t *testing.T) {
	dir := hookTestEnv(t)
	old := hookParentIdentity
	t.Cleanup(func() { hookParentIdentity = old })
	hookParentIdentity = func() (int32, time.Time, error) { panic("fixture parent panic") }
	var out, stderr bytes.Buffer
	if code := run([]string{"hook", "copilot", "sessionStart"}, "test", strings.NewReader(`{"sessionId":"s","timestamp":1,"cwd":"/repo"}`), &out, &stderr); code != 0 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "hook-errors.log"))
	if err != nil || !bytes.Contains(b, []byte("panic")) {
		t.Fatal(string(b), err)
	}
}

func TestHookMalformedArgumentsStillExitZero(t *testing.T) {
	hookTestEnv(t)
	for _, args := range [][]string{{"hook"}, {"hook", "copilot"}, {"hook", "unknown", "event"}} {
		var out, stderr bytes.Buffer
		if code := run(args, "test", strings.NewReader("{}"), &out, &stderr); code != 0 || out.Len() != 0 {
			t.Fatal(args, code, out.String())
		}
	}
}
