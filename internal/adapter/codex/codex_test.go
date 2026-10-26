package codex_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/tail"
	_ "modernc.org/sqlite"
)

const sid = "12345678-1234-4234-8234-123456789abc"
const filename = "rollout-2026-10-18T12-00-00-" + sid + ".jsonl"

type offsets map[string]model.Offset

func (s offsets) GetOffset(path string) (model.Offset, bool, error) {
	o, ok := s[path]
	return o, ok, nil
}
func (s offsets) SetOffset(path string, o model.Offset) error { s[path] = o; return nil }

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "codex", "0.16", "tune", filename))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func setup(t *testing.T, data string) (adapter.Env, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("CODEX_HOME", "")
	path := filepath.Join(home, ".codex", "sessions", "2026", "10", "18", filename)
	write(t, path, data)
	return adapter.Env{Home: home, Now: time.Now(), Offsets: offsets{}}, path
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, a adapter.Adapter, env adapter.Env) (model.SessionObs, []adapter.Warning) {
	t.Helper()
	obs, warnings, err := a.Collect(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 1 {
		t.Fatalf("observations = %v, warnings = %v", obs, warnings)
	}
	return obs[0], warnings
}

func warningCount(warnings []adapter.Warning, kind string) int {
	n := 0
	for _, w := range warnings {
		if w.Kind == kind {
			n++
		}
	}
	return n
}

func TestRolloutMeta(t *testing.T) {
	env, path := setup(t, fixture(t))
	a := codex.New()
	obs, w := collect(t, a, env)
	if a.Name() != "codex" || !reflect.DeepEqual(a.TestedVersions(), []string{"0.16"}) {
		t.Fatal("adapter identity")
	}
	if obs.Adapter != "codex" || obs.ExternalID != sid || obs.Cwd != "/workspace/sample" || obs.Branch != "feat/example" || obs.Version != "0.16.2" || obs.Origin != "interactive" || obs.Hosted || obs.PID != 0 || obs.SourcePath != path {
		t.Fatalf("meta = %+v", obs)
	}
	if !obs.StartedAt.Equal(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)) || !obs.LastActivityAt.Equal(time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC)) {
		t.Fatalf("timestamps: %+v", obs)
	}
	if warningCount(w, "untested_version") != 0 {
		t.Fatal(w)
	}
}

func TestExecOriginHeadless(t *testing.T) {
	for _, source := range []string{`"exec"`, `{"kind":"exec"}`} {
		t.Run(source, func(t *testing.T) {
			data := strings.Replace(fixture(t), `"source":"cli"`, `"source":`+source, 1)
			env, _ := setup(t, data)
			obs, _ := collect(t, codex.New(), env)
			if obs.Origin != "headless" || obs.Hosted {
				t.Fatalf("origin = %+v", obs)
			}
		})
	}
}

func TestHostedOrigin(t *testing.T) {
	for _, origin := range []string{"codex_vscode", "Codex Desktop", "app-server", "plugin", "mcp"} {
		t.Run(origin, func(t *testing.T) {
			env, _ := setup(t, strings.Replace(fixture(t), "codex_cli_rs", origin, 1))
			obs, _ := collect(t, codex.New(), env)
			if obs.Origin != "hosted" || !obs.Hosted {
				t.Fatalf("origin = %+v", obs)
			}
		})
	}
	for _, source := range []string{`"vscode"`, `"mcp"`, `"app-server"`, `{"kind":"plugin"}`} {
		t.Run(source, func(t *testing.T) {
			env, _ := setup(t, strings.Replace(fixture(t), `"source":"cli"`, `"source":`+source, 1))
			obs, _ := collect(t, codex.New(), env)
			if obs.Origin != "hosted" || !obs.Hosted {
				t.Fatalf("origin = %+v", obs)
			}
		})
	}
}

func TestTurnLifecycle(t *testing.T) {
	for _, event := range []string{"task_complete", "turn_complete", "turn_completed"} {
		t.Run(event, func(t *testing.T) {
			data := strings.Replace(fixture(t), "task_complete", event, 1)
			idx := strings.LastIndex(data, `{"timestamp"`)
			env, path := setup(t, data[:idx])
			a := codex.New()
			obs, _ := collect(t, a, env)
			if !obs.TurnOpen || len(obs.Completions) != 0 {
				t.Fatalf("open: %+v", obs)
			}
			write(t, path, data)
			obs, _ = collect(t, a, env)
			wantID := fmt.Sprintf("codex:%s:%d", sid, idx)
			if obs.TurnOpen || len(obs.Completions) != 1 || obs.Completions[0].ID != wantID || obs.Completions[0].Text != "" || obs.Completions[0].TS.IsZero() {
				t.Fatalf("complete: %+v", obs)
			}
			env.Offsets = offsets{}
			obs, _ = collect(t, a, env)
			if len(obs.Completions) != 1 || obs.Completions[0].ID != wantID {
				t.Fatalf("reparse: %+v", obs)
			}
			// Replacing the file resets its inode and must preserve absolute IDs.
			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}
			write(t, path, data)
			obs, _ = collect(t, a, env)
			if len(obs.Completions) != 1 || obs.Completions[0].ID != wantID {
				t.Fatalf("reset: %+v", obs)
			}
		})
	}
}

func TestApplyPatchFiles(t *testing.T) {
	env, path := setup(t, fixture(t))
	a := codex.New()
	obs, _ := collect(t, a, env)
	if len(obs.FilesTouched) != 2 || obs.FilesTouched[0].Path != "/workspace/sample/a.go" || obs.FilesTouched[1].Path != "/workspace/sample/b.go" {
		t.Fatalf("files = %v", obs.FilesTouched)
	}
	data := fixture(t) + `{"timestamp":"2026-10-18T12:00:06Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Delete File: old.go\n*** Move to: moved.go\n*** Update File: /absolute/file.go"}}` + "\n"
	write(t, path, data)
	// Even a fresh adapter using stored offsets must resolve relative paths.
	obs, _ = collect(t, codex.New(), env)
	want := []string{"/workspace/sample/old.go", "/workspace/sample/moved.go", "/absolute/file.go"}
	var got []string
	for _, touch := range obs.FilesTouched {
		got = append(got, touch.Path)
		if touch.TS.IsZero() {
			t.Fatal("missing timestamp")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func catalog(t *testing.T, env adapter.Env, ddl string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(env.Home, ".codex", "codex-dev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogTitle(t *testing.T) {
	env, _ := setup(t, fixture(t))
	catalog(t, env, `CREATE TABLE local_thread_catalog(id TEXT, display_title TEXT, cwd TEXT); INSERT INTO local_thread_catalog VALUES ('`+sid+`', 'Synthetic title', '/workspace/sample')`)
	obs, w := collect(t, codex.New(), env)
	if obs.Title != "Synthetic title" || warningCount(w, "catalog_unavailable") != 0 {
		t.Fatalf("title=%q warnings=%v", obs.Title, w)
	}
}

func TestCatalogMissingIsWarning(t *testing.T) {
	for _, ddl := range []string{"", `CREATE TABLE other(id TEXT)`, `CREATE TABLE local_thread_catalog(id TEXT)`, `CREATE TABLE local_thread_catalog(display_title TEXT)`} {
		t.Run(ddl, func(t *testing.T) {
			env, _ := setup(t, fixture(t))
			if ddl != "" {
				catalog(t, env, ddl)
			}
			_, w := collect(t, codex.New(), env)
			if warningCount(w, "catalog_unavailable") != 1 {
				t.Fatalf("warnings = %v", w)
			}
			if ddl == "" {
				if _, err := os.Stat(filepath.Join(env.Home, ".codex", "codex-dev.db")); !os.IsNotExist(err) {
					t.Fatalf("missing catalog created: %v", err)
				}
			}
		})
	}
}

func TestParseToleratesUnknownTypes(t *testing.T) {
	env, _ := setup(t, fixture(t)+"{\"type\":\"future-thing\"}\nnot json\n{}\n"+
		`{"type":"event_msg","payload":{"type":"future-event","message":{"new":"shape"}}}`+"\n"+
		`{"type":"response_item","payload":{"type":"future-item","input":{"new":"shape"}}}`+"\n")
	obs, w := collect(t, codex.New(), env)
	if obs.ExternalID != sid || warningCount(w, "invalid_json") != 1 || len(w) != 2 {
		t.Fatalf("obs=%+v warnings=%v", obs, w)
	}
}

func TestSnippetsOptIn(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(fmt.Sprint(capture), func(t *testing.T) {
			env, _ := setup(t, fixture(t))
			env.CaptureSnippets = capture
			obs, _ := collect(t, codex.New(), env)
			if obs.LastPromptAt.IsZero() {
				t.Fatal("missing LastPromptAt")
			}
			if capture {
				if obs.LastPrompt != "Please update [redacted]" || obs.Completions[0].Text != "Finished using [redacted]" {
					t.Fatalf("snippets = %+v", obs)
				}
			} else if obs.LastPrompt != "" || obs.Completions[0].Text != "" {
				t.Fatal("captured text without opt-in")
			}
		})
	}
}

func TestDiscoveryBaselineAndHome(t *testing.T) {
	env, path := setup(t, fixture(t))
	old := env.Now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	env.Baseline = true
	obs, _, err := codex.New().Collect(context.Background(), env)
	o, ok, _ := env.Offsets.GetOffset(path)
	if err != nil || len(obs) != 0 || !ok || o.Pos != int64(len(fixture(t))) {
		t.Fatalf("baseline obs=%v offset=%+v err=%v", obs, o, err)
	}
	env.Baseline = false
	obs, _, err = codex.New().Collect(context.Background(), env)
	if err != nil || len(obs) != 0 {
		t.Fatalf("old rollout obs=%v err=%v", obs, err)
	}
	custom := t.TempDir()
	t.Setenv("CODEX_HOME", custom)
	write(t, filepath.Join(custom, "sessions", "2026", "10", "18", filename), fixture(t))
	got, _ := collect(t, codex.New(), env)
	if !strings.HasPrefix(got.SourcePath, custom) {
		t.Fatal(got.SourcePath)
	}
}

func TestTemporaryCwdAndVersion(t *testing.T) {
	for _, cwd := range []string{"/private/tmp/example", "/tmp/example", filepath.Join(os.TempDir(), "example")} {
		t.Run(cwd, func(t *testing.T) {
			quoted, err := json.Marshal(cwd)
			if err != nil {
				t.Fatal(err)
			}
			data := strings.ReplaceAll(strings.ReplaceAll(fixture(t), "/workspace/sample", string(quoted[1:len(quoted)-1])), "0.16.2", "9.0.0")
			env, _ := setup(t, data)
			obs, w := collect(t, codex.New(), env)
			if obs.Origin != "interactive/temporary" || warningCount(w, "untested_version") != 1 {
				t.Fatalf("obs=%+v warnings=%v", obs, w)
			}
		})
	}
}

func TestOversizeOffsetsAndBudget(t *testing.T) {
	data := fixture(t)
	idx := strings.LastIndex(data, `{"timestamp"`)
	// Crosses the 8 MiB budget and skips the oversized record at a boundary.
	big := strings.Repeat("x", 9<<20) + "\n"
	env, path := setup(t, data[:idx]+big+data[idx:])
	a := codex.New()
	first, w := collect(t, a, env)
	o, _, _ := env.Offsets.GetOffset(path)
	if o.Pos != o.Size || warningCount(w, "skipped_backlog") != 1 || warningCount(w, "invalid_json") != 0 || len(first.Completions) != 1 || first.Completions[0].ID != fmt.Sprintf("codex:%s:%d", sid, idx+len(big)) {
		t.Fatalf("budget offset=%d completions=%v", o.Pos, first.Completions)
	}
	obs, w := collect(t, a, env)
	if warningCount(w, "skipped_backlog") != 0 || len(obs.Completions) != 0 {
		t.Fatalf("obs=%+v warnings=%v", obs, w)
	}
	// Also verify a complete skipped record followed by an event in one read.
	env, _ = setup(t, data[:idx]+strings.Repeat("x", tail.MaxLine+1)+"\n"+data[idx:])
	obs, w = collect(t, a, env)
	if warningCount(w, "oversize_line") != 1 || len(obs.Completions) != 1 || obs.Completions[0].ID != fmt.Sprintf("codex:%s:%d", sid, idx+tail.MaxLine+2) {
		t.Fatalf("obs=%+v warnings=%v", obs, w)
	}
}

func TestFilenameFallback(t *testing.T) {
	env, _ := setup(t, strings.ReplaceAll(fixture(t), `"id":"`+sid+`",`, ""))
	obs, _ := collect(t, codex.New(), env)
	if obs.ExternalID != sid || !strings.HasPrefix(obs.Completions[0].ID, "codex:"+sid+":") {
		t.Fatalf("obs=%+v", obs)
	}
}

func TestRolloutPanelMetadataWithoutSnippets(t *testing.T) {
	env, _ := setup(t, fixture(t))
	o, _ := collect(t, codex.New(), env)
	tools, starts, ends := 0, 0, 0
	for _, e := range o.Events {
		if strings.HasPrefix(e.Kind, "log:tool_call:") {
			tools++
		}
		if e.Kind == "log:turn_start" {
			starts++
		}
		if e.Kind == "log:turn_end" {
			ends++
		}
		if e.Summary != "" {
			t.Fatal("captured rollout text")
		}
	}
	if tools != 1 || starts != 1 || ends != 1 {
		t.Fatalf("metadata %d tools %d starts %d ends: %+v", tools, starts, ends, o.Events)
	}
}
