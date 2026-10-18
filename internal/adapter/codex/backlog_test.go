package codex

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
	t.Setenv("CODEX_HOME", "")
	return adapter.Env{Home: home, Now: time.Date(2026, 10, 18, 12, 20, 2, 0, time.UTC), Offsets: backlogOffsets{}},
		filepath.Join(home, ".codex", "sessions", "2026", "10", "18", "rollout-example.jsonl")
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

const backlogHead = `{"timestamp":"2026-10-18T12:00:00Z","type":"session_meta","payload":{"id":"head-session","cwd":"/workspace/example","cli_version":"0.16.2","originator":"plugin","git":{"branch":"feat/recent"}}}
`
const backlogTail = `{"timestamp":"2026-10-18T12:20:00Z","type":"event_msg","payload":{"type":"task_started"}}
{"timestamp":"2026-10-18T12:20:01Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Update File: recent.go"}}
`
const backlogCompletion = `{"timestamp":"2026-10-18T12:20:02Z","type":"event_msg","payload":{"type":"task_complete"}}
`

func backlogPadding() string {
	return strings.Repeat(`{"type":"future","payload":"`+strings.Repeat("x", 128)+`"}`+"\n", 20)
}

func assertRecentBacklog(t *testing.T, env adapter.Env, path, data string, from int64, complete bool, obs model.SessionObs, ws []adapter.Warning) {
	t.Helper()
	wantTime, _ := time.Parse(time.RFC3339, "2026-10-18T12:20:01Z")
	if complete {
		wantTime = wantTime.Add(time.Second)
	}
	if !obs.LastActivityAt.Equal(wantTime) || obs.TurnOpen == complete || len(obs.FilesTouched) != 1 || obs.FilesTouched[0].Path != "/workspace/example/recent.go" {
		t.Fatalf("recent tail not folded: %+v", obs)
	}
	start, _ := time.Parse(time.RFC3339, "2026-10-18T12:00:00Z")
	if obs.ExternalID != "head-session" || obs.Cwd != "/workspace/example" || obs.Branch != "feat/recent" || obs.Version != "0.16.2" || !obs.Hosted || obs.Origin != "hosted" || !obs.StartedAt.Equal(start) {
		t.Fatalf("head session_meta lost: %+v", obs)
	}
	if complete {
		wantID := fmt.Sprintf("codex:head-session:%d", strings.Index(data, backlogCompletion))
		if len(obs.Completions) != 1 || obs.Completions[0].ID != wantID || !obs.Completions[0].TS.Equal(wantTime) {
			t.Fatalf("completion=%+v want %s", obs.Completions, wantID)
		}
	} else if len(obs.Completions) != 0 {
		t.Fatal(obs.Completions)
	}
	target := int64(len(data)) - maxBytes
	boundary := target + int64(strings.IndexByte(data[target-1:], '\n'))
	want := fmt.Sprintf("%s: %d bytes", path, boundary-from)
	var skips int
	for _, w := range ws {
		if w.Kind == "catalog-unavailable" {
			continue
		}
		if w.Kind != "skipped-backlog" || w.Detail != want {
			t.Fatalf("unexpected warning %v, want skipped-backlog %q", w, want)
		}
		skips++
	}
	if skips != 1 {
		t.Fatalf("warnings=%v", ws)
	}
	o, ok, err := env.Offsets.GetOffset(path)
	if err != nil || !ok || o.Pos != int64(len(data)) {
		t.Fatalf("offset=%+v exists=%v err=%v", o, ok, err)
	}
}

func TestFirstImportLargeRolloutFoldsRecentTail(t *testing.T) {
	old := maxBytes
	maxBytes = 1024
	t.Cleanup(func() { maxBytes = old })
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			env, path := backlogEnv(t)
			data := backlogHead + backlogPadding() + backlogTail
			if complete {
				data += backlogCompletion
			}
			writeBacklog(t, path, data)
			obs, ws := collectBacklog(t, env)
			assertRecentBacklog(t, env, path, data, 0, complete, obs, ws)
		})
	}
}

func TestBacklogLargerThanBudgetSkipsAhead(t *testing.T) {
	old := maxBytes
	maxBytes = 1024
	t.Cleanup(func() { maxBytes = old })
	env, path := backlogEnv(t)
	writeBacklog(t, path, backlogHead)
	collectBacklog(t, env)
	prev, _, _ := env.Offsets.GetOffset(path)
	data := backlogHead + backlogPadding() + backlogTail + backlogCompletion
	writeBacklog(t, path, data)
	obs, ws := collectBacklog(t, env)
	assertRecentBacklog(t, env, path, data, prev.Pos, true, obs, ws)
	obs, ws = collectBacklog(t, env)
	if len(obs.Completions) != 0 || len(obs.FilesTouched) != 0 {
		t.Fatalf("tail replayed: %+v %v", obs, ws)
	}
}

func TestBaselineSkipsLargeOldRollout(t *testing.T) {
	old := maxBytes
	maxBytes = 1024
	t.Cleanup(func() { maxBytes = old })
	env, path := backlogEnv(t)
	env.Baseline = true
	data := backlogHead + backlogPadding() + backlogTail + backlogCompletion
	writeBacklog(t, path, data)
	mtime := env.Now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	obs, ws, err := New().Collect(context.Background(), env)
	if err != nil || len(obs) != 0 {
		t.Fatalf("obs=%v warnings=%v err=%v", obs, ws, err)
	}
	for _, w := range ws {
		if w.Kind != "catalog-unavailable" {
			t.Fatalf("unexpected warning: %v", w)
		}
	}
	o, ok, err := env.Offsets.GetOffset(path)
	if err != nil || !ok || o.Pos != int64(len(data)) {
		t.Fatalf("offset=%+v exists=%v err=%v", o, ok, err)
	}
}
