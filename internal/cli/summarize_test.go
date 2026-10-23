package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

const claudeHelp = "-p, --print --safe-mode --no-session-persistence --tools --output-format --json-schema --model --max-budget-usd --name --setting-sources --strict-mcp-config --mcp-config --allowedTools --verbose --include-partial-messages --resume"

func fakeClaude(t *testing.T, help, body string) string {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	t.Helper()
	dir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gitPath, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_RECORD", filepath.Join(dir, "record"))
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo version >> \"$CLAUDE_RECORD.probes\"; echo 2.1.303; exit;;\n--help) echo help >> \"$CLAUDE_RECORD.probes\"; printf '%s\\n' '" + help + "'; exit;;\nesac\nprintf '%s\\n' \"$@\" > \"$CLAUDE_RECORD.args\"\n/bin/cat > \"$CLAUDE_RECORD.stdin\"\n/bin/cat \"$CLAUDE_RECORD.stdin\" >> \"$CLAUDE_RECORD.inputs\"\nprintf 'call\\n' >> \"$CLAUDE_RECORD.calls\"\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("COPILOT_HOME", t.TempDir())
	return filepath.Join(dir, "record")
}

func writeSummaryConfig(t *testing.T, capture string) {
	t.Helper()
	text := "capture_text = '" + capture + "'\n[summaries]\nenabled = true\n"
	if err := os.WriteFile(config.Path(), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func summaryFixture(t *testing.T) (*store.Store, string, int64) {
	t.Helper()
	readEnv(t)
	dir := filepath.Join(gitscan.NormalizePath(t.TempDir()), "fixture")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "init", "-b", "main")
	gitTest(t, dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-m", "first subject")
	gitTest(t, dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-m", "second subject")
	var repo int64
	st := readDB(t, func(tx *store.Tx) error {
		var err error
		repo, err = tx.UpsertRepo(model.Repo{Name: "fixture", Path: dir})
		if err != nil {
			return err
		}
		wts, scanErr := gitscan.NewScanner(1).Scan(context.Background(), dir, gitscan.Full)
		if scanErr != nil || len(wts) != 1 {
			t.Fatalf("scan: %+v %v", wts, scanErr)
		}
		wts[0].RepoID = repo
		if _, err = tx.UpsertWorktree(wts[0]); err != nil {
			return err
		}
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "s", Cwd: dir, Title: "recent title", LastPrompt: "TRANSCRIPT-SENTINEL", LastActivityAt: time.Now()}, RepoID: &repo})
		if err != nil {
			return err
		}
		return tx.InsertHandoff(id, time.Now(), "HANDOFF-SENTINEL")
	})
	return st, dir, repo
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
}

func TestSummarizeDisabled(t *testing.T) {
	readEnv(t)
	record := fakeClaude(t, claudeHelp, "exit 99")
	var out, errout bytes.Buffer
	code := run([]string{"projects", "summarize", "--all"}, "test", nil, &out, &errout)
	want := "summaries are off; set [summaries] enabled = true in " + config.Path()
	if code == 0 || !strings.Contains(errout.String(), want) {
		t.Fatal(code, errout.String())
	}
	if _, err := os.Stat(record + ".probes"); !os.IsNotExist(err) {
		t.Fatal("claude ran", err)
	}
}

func TestSummarizeArgsAndInputs(t *testing.T) {
	for _, capture := range []string{"titles", "snippets"} {
		t.Run(capture, func(t *testing.T) {
			st, dir, _ := summaryFixture(t)
			writeSummaryConfig(t, capture)
			record := fakeClaude(t, claudeHelp, `printf '%s\n' '{"type":"result","structured_output":{"summary":"Ready to review."},"result":"ignored"}'`)
			readRun(t, "projects", "summarize", "fixture", "--json")
			data, err := os.ReadFile(record + ".args")
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(args) != 15 {
				t.Fatalf("args=%q", args)
			}
			var schema map[string]any
			if err := json.Unmarshal([]byte(args[8]), &schema); err != nil {
				t.Fatal(err)
			}
			want := []string{"-p", "--safe-mode", "--no-session-persistence", "--tools", "", "--output-format", "json", "--json-schema", args[8], "--model", "haiku", "--max-budget-usd", "0.05", "--name", "driga-internal"}
			// Schema is the value immediately after --json-schema.
			want[8] = args[8]
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("args=%q want=%q", args, want)
			}
			input, err := os.ReadFile(record + ".inputs")
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"fixture", "main", "first subject", "second subject", "recent title"} {
				if !bytes.Contains(input, []byte(text)) {
					t.Fatalf("missing %s: %s", text, input)
				}
			}
			if bytes.Contains(input, []byte("TRANSCRIPT-SENTINEL")) || bytes.Contains(input, []byte(dir)) {
				t.Fatalf("private input: %s", input)
			}
			if bytes.Contains(input, []byte("HANDOFF-SENTINEL")) != (capture == "snippets") {
				t.Fatalf("handoff gate: %s", input)
			}
			summaries, err := st.Summaries(0)
			if err != nil || len(summaries) != 0 {
				t.Fatal("CLI wrote DB", summaries, err)
			}
			events, _, err := spool.Drain(config.SpoolDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 1
			if capture == "snippets" {
				wantCalls = 2
			}
			if len(events) != wantCalls {
				t.Fatal(events)
			}
			for i, ev := range events {
				if ev.Event != "summary" || ev.Summary == nil || ev.Summary.UsedSnippets != (capture == "snippets" && i == 0) {
					t.Fatal("variant provenance", events)
				}
			}
			lastInput, err := os.ReadFile(record + ".stdin")
			if err != nil || bytes.Contains(lastInput, []byte("HANDOFF-SENTINEL")) {
				t.Fatal("titles-only call contained snippets", string(lastInput), err)
			}
			probes, err := os.ReadFile(record + ".probes")
			if err != nil || string(probes) != "version\nhelp\n" {
				t.Fatal("probe must run once", string(probes), err)
			}
			calls, _ := os.ReadFile(record + ".calls")
			if strings.Count(string(calls), "call") != wantCalls {
				t.Fatal(string(calls))
			}
		})
	}
}

func TestSummarizeBadOutput(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"non-json", `echo nonsense`, "bad claude output"},
		{"missing", `echo '{"type":"result","result":"{}"}'`, "summary"},
		{"bare", `echo '{"summary":"not an envelope"}'`, "bad claude output"},
		{"empty", `echo '{"result":"{\"summary\":\" \"}"}'`, "summary"},
		{"structured-invalid", `echo '{"structured_output":{},"result":"{\"summary\":\"fallback forbidden\"}"}'`, "summary"},
		{"nonzero", `echo broken >&2; exit 9`, "broken"},
		{"missing-flag", `exit 99`, "lacks --safe-mode"},
		{"missing-claude", `exit 99`, "find claude"},
		{"second-call-fails", `if [ -f "$CLAUDE_RECORD.once" ]; then echo bad; else : > "$CLAUDE_RECORD.once"; echo '{"result":"{\"summary\":\"first succeeds\"}"}'; fi`, "bad claude output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summaryFixture(t)
			writeSummaryConfig(t, "snippets")
			help := claudeHelp
			if tc.name == "missing-flag" {
				help = strings.ReplaceAll(help, "--safe-mode", "")
			}
			fakeClaude(t, help, tc.body)
			if tc.name == "missing-claude" {
				t.Setenv("PATH", t.TempDir())
			}
			var out, errout bytes.Buffer
			if code := run([]string{"projects", "summarize", "fixture"}, "test", nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), tc.want) {
				t.Fatal(code, out.String(), errout.String())
			}
			events, _, err := spool.Drain(config.SpoolDir(), nil)
			if err != nil || len(events) != 0 {
				t.Fatal("partial spool", events, err)
			}
		})
	}
}

func TestSummaryIngestAndStale(t *testing.T) {
	st, dir, repo := summaryFixture(t)
	writeSummaryConfig(t, "titles")
	fakeClaude(t, claudeHelp, `echo '{"result":"{\"summary\":\"Ready to review.\"}"}'`)
	readRun(t, "projects", "summarize", "fixture")
	cfg, err := config.Load(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Pass(context.Background(), collector.Deps{Cfg: cfg, SpoolDir: config.SpoolDir()}, st, collector.Plan{}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Summaries(repo)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	out, _ := readRun(t, "projects")
	if !strings.Contains(out, "Ready to review. · claude") || strings.Contains(out, "(stale)") {
		t.Fatal(out)
	}
	gitTest(t, dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-m", "new commit")
	cfg.Roots = []string{dir}
	if _, err := collector.Pass(context.Background(), collector.Deps{Cfg: cfg, SpoolDir: config.SpoolDir()}, st, collector.Plan{Git: true, GitFull: true}); err != nil {
		t.Fatal(err)
	}
	out, _ = readRun(t, "projects")
	if !strings.Contains(out, "(stale)") {
		t.Fatal(out)
	}
}

func TestSummariesConfigShowRoundTrip(t *testing.T) {
	readEnv(t)
	writeSummaryConfig(t, "titles")
	out, _ := readRun(t, "config", "show")
	if !strings.Contains(out, "[summaries]") || !strings.Contains(out, "model = 'haiku'") {
		t.Fatal(out)
	}
	if err := os.WriteFile(config.Path(), []byte(out), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Path())
	if err != nil || !cfg.Summaries.Enabled || cfg.Summaries.Model != "haiku" {
		t.Fatal(cfg, err)
	}
}

// A worktree removed after collection has no commits to read; it is
// summarized from its stored state instead of failing the project.
func TestSummarizeSkipsMissingWorktree(t *testing.T) {
	st, dir, repo := summaryFixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: dir + "-gone", Branch: "removed-branch"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	writeSummaryConfig(t, "titles")
	record := fakeClaude(t, claudeHelp, `printf '%s\n' '{"type":"result","structured_output":{"summary":"Ready to review."}}'`)
	readRun(t, "projects", "summarize", "fixture")
	input, err := os.ReadFile(record + ".inputs")
	if err != nil || !bytes.Contains(input, []byte("removed-branch")) || !bytes.Contains(input, []byte("second subject")) {
		t.Fatalf("input: %s %v", input, err)
	}
}
