package hooks_test

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/hooks"
)

func TestUnifiedMinimalEditsAndHunks(t *testing.T) {
	for _, tc := range []struct{ name, before, after, want string }{
		{"replace", "old\n", "new\n", "@@ -1,1 +1,1 @@\n-old\n+new\n"},
		{"create", "", "new\n", "@@ -0,0 +1,1 @@\n+new\n"},
		{"delete", "old\n", "", "@@ -1,1 +0,0 @@\n-old\n"},
		{"foreign between edits", "start\n/old/driga Stop\nforeign PreToolUse\n/old/driga SessionEnd\nend\n", "start\n/new/driga Stop\nforeign PreToolUse\n/new/driga SessionEnd\nend\n", "@@ -1,5 +1,5 @@\n start\n-/old/driga Stop\n+/new/driga Stop\n foreign PreToolUse\n-/old/driga SessionEnd\n+/new/driga SessionEnd\n end\n"},
		{"separate hunks", "old\n1\n2\n3\n4\n5\n6\n7\nold\n", "new\n1\n2\n3\n4\n5\n6\n7\nnew\n", "@@ -1,4 +1,4 @@\n-old\n+new\n 1\n 2\n 3\n@@ -6,4 +6,4 @@\n 5\n 6\n 7\n-old\n+new\n"},
		{"missing newline", "old", "new\n", "@@ -1,1 +1,1 @@\n-old\n\\ No newline at end of file\n+new\n"},
		{"newline only", "same", "same\n", "@@ -1,1 +1,1 @@\n-same\n\\ No newline at end of file\n+same\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (hooks.Diff{Path: "config", Before: tc.before, After: tc.after, Changed: true}).Unified()
			if want := "--- config\n+++ config\n" + tc.want; got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestClaudeRepairDiffPreservesForeignLines(t *testing.T) {
	path := t.TempDir() + "/settings.json"
	writeFixture(t, path, `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"foreign"}]}]}}`)
	_, err := hooks.ClaudeInstall(path, testBinary("/old/driga"), false, testNow)
	if err != nil {
		t.Fatal(err)
	}
	d, err := hooks.ClaudeInstall(path, testBinary("/new/driga"), true, testNow)
	if err != nil {
		t.Fatal(err)
	}
	minus, plus := 0, 0
	for _, line := range strings.Split(d.Unified(), "\n") {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			minus++
		}
		if strings.HasPrefix(line, "+") {
			plus++
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+") {
			if !strings.Contains(line, "driga") {
				t.Fatalf("unchanged line marked changed: %s", line)
			}
		}
	}
	if minus != 4 || plus != 4 {
		t.Fatalf("want one pair per changed command, got %d/%d:\n%s", minus, plus, d.Unified())
	}
}

func TestEveryHookAndOTLPPreviewUsesMinimalDiff(t *testing.T) {
	for name, install := range map[string]func(string, string, bool, time.Time) (hooks.Diff, error){
		"claude": hooks.ClaudeInstall, "codex": hooks.CodexInstall, "copilot": hooks.CopilotInstall,
	} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir() + "/config"
			old, next := testBinary("/old/driga"), testBinary("/new/driga")
			if _, err := install(path, old, false, testNow); err != nil {
				t.Fatal(err)
			}
			d, err := install(path, next, true, testNow)
			if err != nil {
				t.Fatal(err)
			}
			// Count the path component, which also survives JSON/TOML escaping
			// of Windows backslashes in the rendered settings.
			assertChangedLines(t, d, strings.Count(d.Before, "old"))
		})
	}
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent+" OTLP", func(t *testing.T) {
			path := t.TempDir() + "/config"
			preview := func(endpoint string) hooks.Diff {
				var d hooks.Diff
				var err error
				if agent == "claude" {
					d, err = hooks.ClaudeInstallOTLP(path, false, true, testNow, endpoint)
				} else {
					d, err = hooks.CodexInstallOTLP(path, false, true, testNow, endpoint)
				}
				if err != nil {
					t.Fatal(err)
				}
				return d
			}
			old, next := preview("http://127.0.0.1:7418"), preview("http://127.0.0.1:7419")
			assertChangedLines(t, hooks.Diff{Path: path, Before: old.After, After: next.After, Changed: true}, 1)
		})
	}
}

func assertChangedLines(t *testing.T, d hooks.Diff, want int) {
	t.Helper()
	minus, plus := 0, 0
	for _, line := range strings.Split(d.Unified(), "\n") {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			minus++
		}
		if strings.HasPrefix(line, "+") {
			plus++
		}
	}
	if want == 0 || minus != want || plus != want {
		t.Fatalf("want %d changed line pairs, got %d/%d:\n%s", want, minus, plus, d.Unified())
	}
}
