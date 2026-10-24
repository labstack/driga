package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/labstack/driga/internal/cleanup"
	"strings"
	"testing"
)

func TestCleanupTab(t *testing.T) {
	src := fixture()
	src.snap.LocalMachineID = "local"
	src.snap.Cleanup = []cleanup.Candidate{{ID: "wt:test", Kind: cleanup.Worktree, Path: t.TempDir(), Status: "ready", Evidence: []string{"clean, merged"}}}
	calls := 0
	m := setup(t, src, Options{CleanupApply: func(context.Context, cleanup.Candidate) (cleanup.Result, error) {
		calls++
		return cleanup.Result{}, nil
	}})
	m = key(key(m, '2'), 'd')
	if !strings.Contains(m.View().Content, "reclaim disk") || !strings.Contains(m.View().Content, "ready") {
		t.Fatal(m.View().Content)
	}
	next, cmd := press(m, tea.KeyEnter)
	m = execute(t, next, cmd)
	if !strings.Contains(m.View().Content, "clean, merged") {
		t.Fatal(m.View().Content)
	}
	m = key(m, 'x')
	if calls != 0 || !strings.Contains(m.View().Content, "[y/N]") {
		t.Fatal(calls, m.View().Content)
	}
	m = key(m, 'n')
	if calls != 0 {
		t.Fatal(calls)
	}
	m = key(m, 'x')
	next, cmd = press(m, 'y')
	if cmd == nil || calls != 0 {
		t.Fatal("action must run after confirmation in command", calls)
	}
	m = execute(t, next, cmd)
	if calls != 1 {
		t.Fatal(calls)
	}
	src.snap.Cleanup[0].Status = "needs_review"
	src.version++
	m = execute(t, m, m.(*app).poll())
	m = key(m, tea.KeyEsc)
	m = key(m, 'x')
	if m.(*app).confirm != nil || calls != 1 {
		t.Fatal("needs_review must use explicit CLI include-ignored")
	}
}
