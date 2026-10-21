package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/hooks"
)

var testNow = time.Unix(1800000000, 0)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decodeSettings(t *testing.T, content string) map[string]any {
	t.Helper()
	var m map[string]any
	d := json.NewDecoder(strings.NewReader(content))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestClaudeInstallMergesPreservingExisting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	path := filepath.Join(t.TempDir(), "settings.json")
	before := `{"counter":9007199254740993,"unknown":{"enabled":true},"hooks":{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"other-tool"}]}],"Stop":[{"matcher":"existing","hooks":[{"type":"command","command":"echo stop"}]}]}}`
	writeFixture(t, path, before)
	d, err := hooks.ClaudeInstall(path, "/opt/driga", false, testNow)
	if err != nil || !d.Changed || d.Path != path || d.Before != before || d.After != readFixture(t, path) {
		t.Fatalf("install = %+v, %v", d, err)
	}
	original, got := decodeSettings(t, before), decodeSettings(t, d.After)
	if !reflect.DeepEqual(original["unknown"], got["unknown"]) || got["counter"] != json.Number("9007199254740993") {
		t.Fatalf("unknown values changed: %v", got)
	}
	h := got["hooks"].(map[string]any)
	oldHooks := original["hooks"].(map[string]any)
	if !reflect.DeepEqual(h["PreToolUse"], oldHooks["PreToolUse"]) || !reflect.DeepEqual(h["Stop"].([]any)[0], oldHooks["Stop"].([]any)[0]) {
		t.Fatal("existing hook groups changed")
	}
	for _, event := range []string{"Stop", "Notification", "SessionStart", "SessionEnd"} {
		groups := h[event].([]any)
		group := groups[len(groups)-1].(map[string]any)
		hook := group["hooks"].([]any)[0].(map[string]any)
		if group["matcher"] != "" || hook["type"] != "command" || hook["command"] != "/opt/driga hook claude "+event {
			t.Fatalf("bad %s group: %v", event, group)
		}
	}
	if readFixture(t, path+".driga-bak-1800000000") != before {
		t.Fatal("backup differs")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatalf("mode/format: %v, %q", info.Mode(), d.After)
	}
}

func TestClaudeInstallIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook installation is not supported on Windows in phase 1")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	first, err := hooks.ClaudeInstall(path, "/old/driga", false, testNow)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hooks.ClaudeInstall(path, "/new/driga", false, testNow.Add(time.Second))
	if err != nil || second.Changed || second.Before != first.After || second.After != first.After {
		t.Fatalf("repeat = %+v, %v", second, err)
	}
	backups, _ := filepath.Glob(path + ".driga-bak-*")
	if len(backups) != 0 {
		t.Fatalf("no-op created backups: %v", backups)
	}
}

func TestClaudeUninstallRemovesOnlyDriga(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	before := `{"unknown":42,"hooks":{"Stop":[{"matcher":"mixed","extra":1,"hooks":[{"command":"'/old path/driga' hook claude Stop"},{"command":"echo stay"}]},{"hooks":[{"command":"/old/driga hook claude Stop"}]}],"SessionEnd":[{"hooks":[{"command":"driga hook claude SessionEnd"}]}],"PreToolUse":[{"hooks":[{"command":"/bin/driga-other hook claude Stop"}]}]}}`
	writeFixture(t, path, before)
	d, err := hooks.ClaudeUninstall(path, false)
	if err != nil || !d.Changed {
		t.Fatalf("uninstall = %+v, %v", d, err)
	}
	got := decodeSettings(t, d.After)
	h := got["hooks"].(map[string]any)
	if _, ok := h["SessionEnd"]; ok {
		t.Fatal("empty event retained")
	}
	groups := h["Stop"].([]any)
	group := groups[0].(map[string]any)
	if len(groups) != 1 || group["extra"] != json.Number("1") || len(group["hooks"].([]any)) != 1 || strings.Contains(d.After, "/old") {
		t.Fatalf("uninstall lost unrelated data: %s", d.After)
	}
	if _, ok := h["PreToolUse"]; !ok || got["unknown"] != json.Number("42") {
		t.Fatal("unrelated settings lost")
	}
	backups, _ := filepath.Glob(path + ".driga-bak-*")
	if len(backups) != 1 || readFixture(t, backups[0]) != before {
		t.Fatal("uninstall did not back up original")
	}
	writeFixture(t, path, `{"hooks":{"Stop":[{"hooks":[{"command":"driga hook claude Stop"}]}]}}`)
	d, err = hooks.ClaudeUninstall(path, true)
	if err != nil || strings.Contains(d.After, "hooks") {
		t.Fatalf("empty hooks retained: %+v, %v", d, err)
	}
}

func TestClaudeDryRunNoWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook installation is not supported on Windows in phase 1")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	before := `{"theme":"dark"}`
	writeFixture(t, path, before)
	d, err := hooks.ClaudeInstall(path, "/opt/driga", true, testNow)
	if err != nil || !d.Changed || readFixture(t, path) != before {
		t.Fatalf("dry run = %+v, %v", d, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("dry run wrote backup")
	}
	missing := filepath.Join(dir, "missing", "settings.json")
	if _, err := hooks.ClaudeInstall(missing, "/opt/driga", true, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("dry run created parent")
	}
}

func TestClaudeMissingFileCreates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if _, err := hooks.ClaudeInstall(path, "/opt/driga", false, testNow); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	dir, _ := os.Stat(filepath.Dir(path))
	if info.Mode().Perm() != 0600 || dir.Mode().Perm() != 0700 {
		t.Fatalf("permissions: %v, %v", info.Mode(), dir.Mode())
	}
	installed, err := hooks.ClaudeStatus(path)
	if err != nil || !reflect.DeepEqual(installed, []string{"Notification", "SessionEnd", "SessionStart", "Stop"}) {
		t.Fatalf("status = %v, %v", installed, err)
	}
}

func TestClaudeQuotedBinaryAndPartialInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook installation is not supported on Windows in phase 1")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	writeFixture(t, path, `{"hooks":{"Stop":[{"hooks":[{"command":"driga hook claude Stop"}]}]}}`)
	d, err := hooks.ClaudeInstall(path, "/opt/a user's tools/driga", false, testNow)
	if err != nil {
		t.Fatal(err)
	}
	h := decodeSettings(t, d.After)["hooks"].(map[string]any)
	if len(h["Stop"].([]any)) != 1 {
		t.Fatal("duplicated installed event")
	}
	command := h["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.HasPrefix(command, "'/opt/a user") || !strings.HasSuffix(command, " hook claude SessionStart") {
		t.Fatalf("unquoted binary: %q", command)
	}
	if _, err := hooks.ClaudeUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	if installed, err := hooks.ClaudeStatus(path); err != nil || len(installed) != 0 {
		t.Fatalf("quoted command not removed: %v, %v", installed, err)
	}
}

func TestClaudeInvalidSettingsNoWrite(t *testing.T) {
	for _, before := range []string{`null`, `[]`, `{} {}`, `{"hooks":42}`, `{"hooks":{"Stop":42}}`} {
		t.Run(before, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			writeFixture(t, path, before)
			if _, err := hooks.ClaudeInstall(path, "/opt/driga", false, testNow); err == nil {
				t.Fatal("expected invalid settings error")
			}
			if readFixture(t, path) != before {
				t.Fatal("invalid settings overwritten")
			}
		})
	}
}

func TestDiffUnified(t *testing.T) {
	d := hooks.Diff{Path: "settings.json", Before: "old\n", After: "new\n", Changed: true}
	if got := d.Unified(); !strings.Contains(got, "-old\n") || !strings.Contains(got, "+new\n") {
		t.Fatalf("diff = %q", got)
	}
	if got := (hooks.Diff{Before: "same\n", After: "same\n"}).Unified(); got != "" {
		t.Fatalf("unchanged diff = %q", got)
	}
}

func TestClaudePreservesUnrelatedBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook installation is not supported on Windows in phase 1")
	}
	for _, before := range []string{`{"z":  2, "a" : { "x" : 1 }}`, "{\n\t\"z\": 2,\n\t\"a\": 1\n}\n", "{  }", "{}\n"} {
		for _, otlp := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "settings.json")
			writeFixture(t, path, before)
			var d hooks.Diff
			var err error
			if otlp {
				d, err = hooks.ClaudeInstallOTLP(path, true, false, testNow)
			} else {
				d, err = hooks.ClaudeInstall(path, "/opt/driga", false, testNow)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(d.After, strings.TrimPrefix(before, "{")) {
				t.Fatalf("unrelated bytes changed: %q -> %q", before, d.After)
			}
			if otlp {
				_, err = hooks.ClaudeUninstallOTLP(path, false)
			} else {
				_, err = hooks.ClaudeUninstall(path, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := readFixture(t, path); got != before {
				t.Fatalf("roundtrip: %q != %q", got, before)
			}
		}
	}
}

func TestClaudeDuplicateManagedKeyFallsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook installation is not supported on Windows in phase 1")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	writeFixture(t, path, `{"hooks":{},"hooks":{},"unknown": 2}`)
	d, err := hooks.ClaudeInstall(path, "/opt/driga", true, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !d.FullRewrite || !strings.Contains(d.Unified(), "Full settings rewrite") {
		t.Fatal("ambiguous targeted edit should disclose full rewrite")
	}
	h := decodeSettings(t, d.After)["hooks"].(map[string]any)
	if len(h) != 4 {
		t.Fatalf("hooks not installed: %s", d.After)
	}
}
