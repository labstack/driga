package claude_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/tail"
)

type memoryOffsets struct {
	values map[string]model.Offset
	getErr error
	setErr error
}

func (s *memoryOffsets) GetOffset(path string) (model.Offset, bool, error) {
	o, ok := s.values[path]
	return o, ok, s.getErr
}

func (s *memoryOffsets) SetOffset(path string, o model.Offset) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.values[path] = o
	return nil
}

func testEnv(t *testing.T) adapter.Env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	return adapter.Env{
		Home: home, Now: stamp("2026-10-18T18:00:00Z"),
		Offsets: &memoryOffsets{values: make(map[string]model.Offset)},
		Alive:   func(int32, time.Time) bool { return true },
	}
}

func stamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func writeFile(t *testing.T, env adapter.Env, rel, content string) string {
	t.Helper()
	path := filepath.Join(env.Home, ".claude", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, env.Now, env.Now); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "claude", "2.1", "tune", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func registry(t *testing.T, env adapter.Env) string {
	t.Helper()
	return writeFile(t, env, "sessions/4242.json", fixture(t, "registry.json"))
}

func transcript(t *testing.T, env adapter.Env, content string) string {
	t.Helper()
	return writeFile(t, env, "projects/synthetic/session-a.jsonl", content)
}

func collect(t *testing.T, a adapter.Adapter, env adapter.Env) ([]model.SessionObs, []adapter.Warning) {
	t.Helper()
	obs, warnings, err := a.Collect(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	return obs, warnings
}

func only(t *testing.T, obs []model.SessionObs) model.SessionObs {
	t.Helper()
	if len(obs) != 1 {
		t.Fatalf("want one observation, got %+v", obs)
	}
	return obs[0]
}

func paths(obs model.SessionObs) []string {
	var result []string
	for _, touch := range obs.FilesTouched {
		result = append(result, touch.Path)
	}
	return result
}

func TestRegistryLiveSession(t *testing.T) {
	env := testEnv(t)
	registry(t, env)
	called := false
	env.Alive = func(pid int32, start time.Time) bool {
		called = true
		if pid != 4242 || !start.Equal(time.UnixMilli(1700000000000)) {
			t.Fatalf("wrong liveness identity: %d, %v", pid, start)
		}
		return true
	}
	a := claude.New()
	if a.Name() != "claude" || !reflect.DeepEqual(a.TestedVersions(), []string{"2.1"}) {
		t.Fatalf("adapter identity: %s %v", a.Name(), a.TestedVersions())
	}
	obs, warnings := collect(t, a, env)
	o := only(t, obs)
	if !called || len(warnings) != 0 || o.Adapter != "claude" || o.ExternalID != "session-a" || o.PID != 4242 || o.Cwd != "/r" || o.RegistryStatus != "busy" || o.Version != "2.1.7" || o.Origin != "interactive" || !o.ProcStart.Equal(time.UnixMilli(1700000000000)) || !o.StartedAt.Equal(o.ProcStart) {
		t.Fatalf("observation=%+v warnings=%+v", o, warnings)
	}
}

func TestRegistryPidReuse(t *testing.T) {
	env := testEnv(t)
	registry(t, env)
	env.Alive = func(_ int32, start time.Time) bool { return start.Equal(env.Now) }
	obs, _ := collect(t, claude.New(), env)
	if o := only(t, obs); o.PID != 0 || !o.ProcStart.Equal(time.UnixMilli(1700000000000)) {
		t.Fatalf("stale registry treated as alive: %+v", o)
	}
}

func TestTranscriptFoldsTitleBranchFiles(t *testing.T) {
	env := testEnv(t)
	registry(t, env)
	path := transcript(t, env, fixture(t, "transcript.jsonl"))
	obs, warnings := collect(t, claude.New(), env)
	o := only(t, obs)
	if len(warnings) != 0 || o.Title != "Update example files" || o.Branch != "feat/x" || !reflect.DeepEqual(paths(o), []string{"/r/a.go", "/r/b.go"}) || o.TurnOpen || o.SourcePath != path || !o.LastActivityAt.Equal(stamp("2026-10-18T17:00:04Z")) || !o.LastPromptAt.Equal(stamp("2026-10-18T17:00:00Z")) {
		t.Fatalf("observation=%+v warnings=%+v", o, warnings)
	}
	for _, touch := range o.FilesTouched {
		if !touch.TS.Equal(stamp("2026-10-18T17:00:02Z")) {
			t.Fatalf("wrong touch timestamp: %+v", touch)
		}
	}
}

func TestLastPromptOnlyWithSnippets(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			env := testEnv(t)
			env.CaptureSnippets = enabled
			transcript(t, env, fixture(t, "transcript.jsonl"))
			obs, _ := collect(t, claude.New(), env)
			o := only(t, obs)
			want := ""
			if enabled {
				want = "Use [redacted] for the example."
			}
			if o.LastPrompt != want || strings.Contains(o.LastPrompt, "ghp_") {
				t.Fatalf("prompt=%q want=%q", o.LastPrompt, want)
			}
		})
	}
}

func TestParseToleratesUnknownTypes(t *testing.T) {
	env := testEnv(t)
	path := transcript(t, env, fixture(t, "unknown.jsonl"))
	obs, warnings := collect(t, claude.New(), env)
	want := []adapter.Warning{{Kind: "invalid_json", Detail: path + ": 2 lines"}}
	if only(t, obs).Title != "Still parsed" || !reflect.DeepEqual(warnings, want) {
		t.Fatalf("observations=%+v warnings=%+v", obs, warnings)
	}
}

func TestIncrementalTail(t *testing.T) {
	env := testEnv(t)
	registry(t, env)
	path := transcript(t, env, fixture(t, "transcript.jsonl"))
	a := claude.New()
	collect(t, a, env)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintln(f, `{"type":"assistant","timestamp":"2026-10-18T17:01:00Z","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"next.go"}}]}}`)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append: %v, close: %v", err, closeErr)
	}
	// State belongs to offsets and the collector, not an adapter instance.
	obs, _ := collect(t, claude.New(), env)
	o := only(t, obs)
	if !reflect.DeepEqual(paths(o), []string{"/r/next.go"}) || o.Title != "" || o.Branch != "" || !o.LastPromptAt.IsZero() || o.LastPrompt != "" || o.SourcePath != path {
		t.Fatalf("not a delta: %+v", o)
	}
	obs, _ = collect(t, a, env)
	o = only(t, obs)
	if len(o.FilesTouched) != 0 || !o.LastActivityAt.IsZero() {
		t.Fatalf("unchanged transcript folded again: %+v", o)
	}
}

func TestTemporaryCwd(t *testing.T) {
	for _, cwd := range []string{"/private/tmp/x", "/tmp/x/../y", filepath.Join(os.TempDir(), "synthetic"), "/tmp", "/tmp-other/x"} {
		t.Run(cwd, func(t *testing.T) {
			env := testEnv(t)
			content := strings.Replace(fixture(t, "registry.json"), `"cwd":"/r"`, `"cwd":`+fmt.Sprintf("%q", cwd), 1)
			writeFile(t, env, "sessions/4242.json", content)
			obs, _ := collect(t, claude.New(), env)
			want := "interactive/temporary"
			if cwd == "/tmp-other/x" {
				want = "interactive"
			}
			if o := only(t, obs); o.Origin != want {
				t.Fatalf("origin=%q want=%q", o.Origin, want)
			}
		})
	}
}

func TestUntestedVersionWarning(t *testing.T) {
	for _, version := range []string{"9.0.0", "2.10.0", "2.1", "2.1.99"} {
		t.Run(version, func(t *testing.T) {
			env := testEnv(t)
			content := strings.ReplaceAll(fixture(t, "registry.json"), "2.1.7", version)
			writeFile(t, env, "sessions/4242.json", content)
			writeFile(t, env, "sessions/4243.json", strings.ReplaceAll(content, "session-a", "session-b"))
			obs, warnings := collect(t, claude.New(), env)
			if len(obs) != 2 {
				t.Fatalf("lost unknown-version sessions: %+v", obs)
			}
			want := []adapter.Warning(nil)
			if version == "9.0.0" || version == "2.10.0" {
				want = []adapter.Warning{{Kind: "untested_version", Detail: version}}
			}
			if !reflect.DeepEqual(warnings, want) {
				t.Fatalf("warnings=%+v want=%+v", warnings, want)
			}
		})
	}
}

func TestBaselineSkipsOldFiles(t *testing.T) {
	env := testEnv(t)
	registry(t, env)
	path := transcript(t, env, fixture(t, "transcript.jsonl"))
	unregistered := writeFile(t, env, "projects/synthetic/old.jsonl", fixture(t, "transcript.jsonl"))
	for _, path := range []string{path, unregistered} {
		old := env.Now.Add(-8 * 24 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	env.Baseline = true
	obs, warnings := collect(t, claude.New(), env)
	if len(warnings) != 0 || len(only(t, obs).FilesTouched) != 0 || !only(t, obs).LastActivityAt.IsZero() {
		t.Fatalf("baseline folded historical data: %+v, %+v", obs, warnings)
	}
	for _, path := range []string{path, unregistered} {
		o, ok, err := env.Offsets.GetOffset(path)
		info, statErr := os.Stat(path)
		if err != nil || statErr != nil || !ok || o.Pos != info.Size() || o.Inode == 0 {
			t.Fatalf("baseline offset for %s: %+v %v %v %v", path, o, ok, err, statErr)
		}
	}
}

func TestRegistryToleranceAndOrigins(t *testing.T) {
	for _, tc := range []struct{ entrypoint, kind, want string }{
		{"cli", "interactive", "interactive"}, {"sdk-cli", "interactive", "headless"},
		{"sdk-ts", "interactive", "headless"}, {"sdk-py", "interactive", "headless"},
		{"future-sdk", "interactive", "headless"}, {"unknown", "headless", "headless"},
		{"cli", "print", "headless"}, {"unknown", "interactive", "interactive"},
	} {
		t.Run(tc.entrypoint+"/"+tc.kind, func(t *testing.T) {
			env := testEnv(t)
			content := strings.NewReplacer(`"entrypoint":"cli"`, `"entrypoint":"`+tc.entrypoint+`"`, `"kind":"interactive"`, `"kind":"`+tc.kind+`"`, `1700000000000`, `"2023-11-14T22:13:20Z"`).Replace(fixture(t, "registry.json"))
			writeFile(t, env, "sessions/4242.json", content)
			writeFile(t, env, "sessions/4243.json", "not json")
			writeFile(t, env, "sessions/not-a-pid.json", content)
			writeFile(t, env, "sessions/4244.json", `{}`)
			obs, warnings := collect(t, claude.New(), env)
			o := only(t, obs)
			if o.Origin != tc.want || !o.ProcStart.Equal(time.UnixMilli(1700000000000)) || len(warnings) != 3 {
				t.Fatalf("observation=%+v warnings=%+v", o, warnings)
			}
			for _, w := range warnings {
				if w.Kind != "bad_registry" {
					t.Fatalf("unexpected warning: %+v", w)
				}
			}
		})
	}
}

func TestTranscriptDiscoveryWindow(t *testing.T) {
	env := testEnv(t)
	path := transcript(t, env, fixture(t, "transcript.jsonl"))
	oldPath := writeFile(t, env, "projects/other/old.jsonl", fixture(t, "transcript.jsonl"))
	old := env.Now.Add(-25 * time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	obs, _ := collect(t, claude.New(), env)
	o := only(t, obs)
	if o.ExternalID != "session-a" || o.PID != 0 || o.RegistryStatus != "" || o.SourcePath != path {
		t.Fatalf("unregistered transcript: %+v", o)
	}
	if _, ok, _ := env.Offsets.GetOffset(oldPath); ok {
		t.Fatal("tailed transcript outside the discovery window")
	}
	registry(t, env)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	env.Offsets = &memoryOffsets{values: make(map[string]model.Offset)}
	obs, _ = collect(t, claude.New(), env)
	if len(only(t, obs).FilesTouched) != 2 {
		t.Fatal("registry transcript not discovered beyond 24h")
	}
}

func TestPromptAndTurnLifecycle(t *testing.T) {
	entries := []struct {
		line     string
		open     bool
		promptAt string
	}{
		{`{"type":"user","timestamp":"2026-10-18T17:00:00Z","message":{"content":"real prompt"}}`, true, "2026-10-18T17:00:00Z"},
		{`{"type":"user","timestamp":"2026-10-18T17:00:01Z","isMeta":true,"message":{"content":"metadata"}}`, true, "2026-10-18T17:00:00Z"},
		{`{"type":"user","timestamp":"2026-10-18T17:00:02Z","message":{"content":[{"type":"tool_result","content":"result"}]}}`, true, "2026-10-18T17:00:00Z"},
		{`{"type":"assistant","timestamp":"2026-10-18T17:00:03Z","message":{"stop_reason":"end_turn"}}`, false, "2026-10-18T17:00:00Z"},
		{`{"type":"user","timestamp":"2026-10-18T17:00:04Z","message":{"content":[{"type":"tool_result"}]}}`, false, "2026-10-18T17:00:00Z"},
		{`{"type":"user","timestamp":"2026-10-18T17:00:05Z","message":{"content":[{"type":"text","text":"new prompt"},{"type":"tool_result"}]}}`, true, "2026-10-18T17:00:05Z"},
		{`{"type":"system","timestamp":"2026-10-18T17:00:06Z","stopReason":"stop"}`, false, "2026-10-18T17:00:05Z"},
		{`{"type":"user","timestamp":"2026-10-18T17:00:01Z","message":{"content":"older prompt"}}`, true, "2026-10-18T17:00:05Z"},
	}
	var lines string
	for i, entry := range entries {
		lines += entry.line + "\n"
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			env := testEnv(t)
			transcript(t, env, lines)
			obs, _ := collect(t, claude.New(), env)
			o := only(t, obs)
			if o.TurnOpen != entry.open || !o.LastPromptAt.Equal(stamp(entry.promptAt)) {
				t.Fatalf("turn=%v prompt=%v want %v %s", o.TurnOpen, o.LastPromptAt, entry.open, entry.promptAt)
			}
			if i == len(entries)-1 && !o.LastActivityAt.Equal(stamp("2026-10-18T17:00:06Z")) {
				t.Fatalf("activity regressed: %v", o.LastActivityAt)
			}
		})
	}
}

func TestTranscriptToolsAndAliases(t *testing.T) {
	env := testEnv(t)
	env.CaptureSnippets = true
	transcript(t, env, `{"type":"ai-title","title":"Fallback title","cwd":"/r"}
{"type":"last-prompt","prompt":"  fallback\n prompt ghp_synthetic  "}
{"type":"assistant","timestamp":"2026-10-18T17:00:00Z","message":{"content":[{"type":"tool_use","name":"MultiEdit","input":{"file_path":"dir/a.go"}},{"type":"tool_use","name":"NotebookEdit","input":{"notebook_path":"book.ipynb"}},{"type":"tool_use","name":"Read","input":{"file_path":"ignored"}},{"type":"tool_use","name":"Edit","input":{}},{"type":"tool_use","name":"Write","input":{"file_path":"/absolute.go"}}]}}
`)
	obs, warnings := collect(t, claude.New(), env)
	o := only(t, obs)
	if len(warnings) != 0 || o.Title != "Fallback title" || o.LastPrompt != "fallback prompt [redacted]" || !reflect.DeepEqual(paths(o), []string{"/r/dir/a.go", "/r/book.ipynb", "/absolute.go"}) {
		t.Fatalf("observation=%+v warnings=%+v", o, warnings)
	}
}

func TestTailResetAndPartialLine(t *testing.T) {
	env := testEnv(t)
	path := transcript(t, env, fixture(t, "transcript.jsonl"))
	a := claude.New()
	collect(t, a, env)
	writeFile(t, env, "projects/synthetic/session-a.jsonl", `{"type":"ai-title","aiTitle":"Replacement"}`+"\n"+`{"type":"user","message":{"content":"partial"}}`)
	obs, _ := collect(t, a, env)
	o := only(t, obs)
	if o.Title != "Replacement" || o.Branch != "" || o.TurnOpen || len(o.FilesTouched) != 0 {
		t.Fatalf("reset did not fold from scratch: %+v", o)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n")
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append newline: %v, %v", err, closeErr)
	}
	obs, _ = collect(t, a, env)
	if o := only(t, obs); !o.TurnOpen || o.Title != "" {
		t.Fatalf("partial line was lost or replayed: %+v", o)
	}
}

func TestTranscriptOversizeAndReadBudget(t *testing.T) {
	env := testEnv(t)
	// Valid JSON lines each below MaxLine, together exceeding the 8 MiB budget.
	line := `{"type":"summary","summary":"` + strings.Repeat("x", tail.MaxLine-100) + `"}` + "\n"
	path := transcript(t, env, line+line+line+`{"type":"ai-title","aiTitle":"After budget"}`+"\n")
	a := claude.New()
	obs, warnings := collect(t, a, env)
	offset, _, _ := env.Offsets.GetOffset(path)
	if len(warnings) != 1 || warnings[0].Kind != "skipped_backlog" || offset.Pos != offset.Size || only(t, obs).Title != "After budget" {
		t.Fatalf("recent tail: offset=%+v obs=%+v warnings=%+v", offset, obs, warnings)
	}
	obs, _ = collect(t, a, env)
	if only(t, obs).Title != "" {
		t.Fatal("recent tail replayed after budget skip")
	}
	writeFile(t, env, "projects/synthetic/session-a.jsonl", strings.Repeat("x", tail.MaxLine+1)+"\n"+`{"type":"ai-title","aiTitle":"After oversize"}`+"\n")
	obs, warnings = collect(t, a, env)
	if only(t, obs).Title != "After oversize" || !reflect.DeepEqual(warnings, []adapter.Warning{{Kind: "oversize_line", Detail: path + ": 1 lines"}}) {
		t.Fatalf("oversize: obs=%+v warnings=%+v", obs, warnings)
	}
}

func TestCollectErrorsAndEmptyHome(t *testing.T) {
	env := testEnv(t)
	env.Home = "" // An omitted Home resolves to the isolated HOME.
	obs, warnings := collect(t, claude.New(), env)
	if len(obs) != 0 || len(warnings) != 0 {
		t.Fatalf("empty home: %+v %+v", obs, warnings)
	}
	env.Home = os.Getenv("HOME")
	transcript(t, env, "{}\n")
	sentinel := errors.New("offset unavailable")
	for _, operation := range []string{"get", "set"} {
		t.Run(operation, func(t *testing.T) {
			offsets := &memoryOffsets{values: make(map[string]model.Offset)}
			if operation == "get" {
				offsets.getErr = sentinel
			} else {
				offsets.setErr = sentinel
			}
			env.Offsets = offsets
			_, _, err := claude.New().Collect(context.Background(), env)
			if !errors.Is(err, sentinel) {
				t.Fatalf("offset error not propagated: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := claude.New().Collect(ctx, env)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
}

func TestRegistryRejectsMalformedStartedAt(t *testing.T) {
	env := testEnv(t)
	for i, value := range []any{"invalid", nil, true} {
		var r map[string]any
		if err := json.Unmarshal([]byte(fixture(t, "registry.json")), &r); err != nil {
			t.Fatal(err)
		}
		r["startedAt"] = value
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, env, fmt.Sprintf("sessions/%d.json", 5000+i), string(b))
	}
	obs, warnings := collect(t, claude.New(), env)
	if len(obs) != 0 || len(warnings) != 3 {
		t.Fatalf("malformed registry: %+v %+v", obs, warnings)
	}
}

func TestTranscriptPanelMetadataWithoutSnippets(t *testing.T) {
	env := testEnv(t)
	registry(t, env)
	transcript(t, env, fixture(t, "transcript.jsonl"))
	obs, _ := collect(t, claude.New(), env)
	o := only(t, obs)
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
			t.Fatal("captured transcript text")
		}
	}
	if tools != 2 || starts != 1 || ends != 1 {
		t.Fatalf("metadata %d tools %d starts %d ends: %+v", tools, starts, ends, o.Events)
	}
}
