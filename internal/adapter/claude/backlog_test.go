package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/model"
)

type backlogOffsets map[string]model.Offset

func (s backlogOffsets) GetOffset(path string) (model.Offset, bool, error) {
	o, ok := s[path]
	return o, ok, nil
}
func (s backlogOffsets) SetOffset(path string, o model.Offset) error { s[path] = o; return nil }

func backlogEnv(t *testing.T) (adapter.Env, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	return adapter.Env{Home: home, Now: time.Date(2026, 10, 18, 12, 20, 2, 0, time.UTC), Offsets: backlogOffsets{}},
		filepath.Join(home, ".claude", "projects", "synthetic", "fallback.jsonl")
}

func writeBacklog(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func collectBacklog(t *testing.T, env adapter.Env) (model.SessionObs, []adapter.Warning) {
	t.Helper()
	obs, ws, err := New().Collect(context.Background(), env)
	if err != nil || len(obs) != 1 {
		t.Fatalf("obs=%+v warnings=%v err=%v", obs, ws, err)
	}
	return obs[0], ws
}

const backlogHead = `{"type":"summary","sessionId":"head-session"}
{"type":"summary","cwd":"/workspace/example","gitBranch":"feat/recent"}
{"type":"user","timestamp":"2026-10-18T12:00:00Z","message":{"content":"old prompt"}}
`

const backlogTail = `{"type":"user","timestamp":"2026-10-18T12:20:00Z","message":{"content":"recent prompt"}}
{"type":"assistant","timestamp":"2026-10-18T12:20:01Z","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"recent.go"}}]}}
{"type":"system","timestamp":"2026-10-18T12:20:02Z","stopReason":"stop"}
`

func backlogPadding() string {
	return strings.Repeat(`{"type":"summary","summary":"`+strings.Repeat("x", 128)+`"}`+"\n", 20)
}

func assertRecentBacklog(t *testing.T, env adapter.Env, path, data string, from int64, obs model.SessionObs, ws []adapter.Warning) {
	t.Helper()
	wantTime, _ := time.Parse(time.RFC3339, "2026-10-18T12:20:02Z")
	if !obs.LastActivityAt.Equal(wantTime) || !obs.LastPromptAt.Equal(wantTime.Add(-2*time.Second)) || obs.TurnOpen || len(obs.FilesTouched) != 1 || obs.FilesTouched[0].Path != "/workspace/example/recent.go" {
		t.Fatalf("recent tail not folded: %+v", obs)
	}
	if obs.ExternalID != "head-session" || obs.Cwd != "/workspace/example" || obs.Branch != "feat/recent" {
		t.Fatalf("head identity lost: %+v", obs)
	}
	target := int64(len(data)) - maxTailBytes
	boundary := target + int64(strings.IndexByte(data[target-1:], '\n'))
	want := fmt.Sprintf("%s: %d bytes", path, boundary-from)
	if len(ws) != 1 || ws[0].Kind != "skipped_backlog" || ws[0].Detail != want {
		t.Fatalf("warnings=%v want skipped_backlog %q", ws, want)
	}
	o, ok, err := env.Offsets.GetOffset(path)
	if err != nil || !ok || o.Pos != int64(len(data)) {
		t.Fatalf("offset=%+v exists=%v err=%v", o, ok, err)
	}
}

func TestFirstImportLargeTranscriptFoldsRecentTail(t *testing.T) {
	old := maxTailBytes
	maxTailBytes = 1024
	t.Cleanup(func() { maxTailBytes = old })
	for _, baseline := range []bool{false, true} {
		t.Run(fmt.Sprint(baseline), func(t *testing.T) {
			env, path := backlogEnv(t)
			env.Baseline = baseline
			data := backlogHead + backlogPadding() + backlogTail
			writeBacklog(t, path, data)
			obs, ws := collectBacklog(t, env)
			assertRecentBacklog(t, env, path, data, 0, obs, ws)
		})
	}
}

func TestBacklogLargerThanBudgetSkipsAhead(t *testing.T) {
	old := maxTailBytes
	maxTailBytes = 1024
	t.Cleanup(func() { maxTailBytes = old })
	env, path := backlogEnv(t)
	writeBacklog(t, path, backlogHead)
	collectBacklog(t, env)
	prev, _, _ := env.Offsets.GetOffset(path)
	data := backlogHead + backlogPadding() + backlogTail
	writeBacklog(t, path, data)
	obs, ws := collectBacklog(t, env)
	assertRecentBacklog(t, env, path, data, prev.Pos, obs, ws)
	obs, ws = collectBacklog(t, env)
	if !obs.LastActivityAt.IsZero() || len(obs.FilesTouched) != 0 || len(ws) != 0 {
		t.Fatalf("tail replayed: %+v %v", obs, ws)
	}
}

func TestLargeTranscriptWithoutOffsetStore(t *testing.T) {
	old := maxTailBytes
	maxTailBytes = 1024
	t.Cleanup(func() { maxTailBytes = old })
	env, path := backlogEnv(t)
	env.Offsets = nil
	writeBacklog(t, path, backlogHead+backlogPadding()+backlogTail)
	obs, ws := collectBacklog(t, env)
	if !obs.LastActivityAt.Equal(env.Now) || obs.ExternalID != "head-session" || obs.Cwd != "/workspace/example" || obs.Branch != "feat/recent" || len(obs.FilesTouched) != 1 || len(ws) != 1 || ws[0].Kind != "skipped_backlog" {
		t.Fatalf("obs=%+v warnings=%v", obs, ws)
	}
}

func TestBaselineSkipsLargeOldTranscript(t *testing.T) {
	old := maxTailBytes
	maxTailBytes = 1024
	t.Cleanup(func() { maxTailBytes = old })
	env, path := backlogEnv(t)
	env.Baseline = true
	data := backlogHead + backlogPadding() + backlogTail
	writeBacklog(t, path, data)
	mtime := env.Now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	obs, ws, err := New().Collect(context.Background(), env)
	if err != nil || len(obs) != 0 || len(ws) != 0 {
		t.Fatalf("obs=%v warnings=%v err=%v", obs, ws, err)
	}
	o, ok, err := env.Offsets.GetOffset(path)
	if err != nil || !ok || o.Pos != int64(len(data)) {
		t.Fatalf("offset=%+v exists=%v err=%v", o, ok, err)
	}
}
