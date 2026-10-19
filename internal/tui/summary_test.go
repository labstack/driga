package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestSummaryKeyRunsOnlyOnDemand(t *testing.T) {
	src := fixture()
	src.snap.LocalMachineID = "local"
	src.snap.Projects.Rows = []view.ProjectRow{{MachineID: "local", Worktrees: 1, Status: view.ProjectStatus{Repo: model.Repo{ID: 1, Name: "fixture", Path: "/fixture"}}}}
	src.snap.ProjectDetails = map[int64]view.WhereView{1: {WorktreeRows: []view.WorktreeRow{{Worktree: model.Worktree{Path: "/linked", Branch: "topic"}}}}}
	calls := 0
	m := setup(t, src, Options{Summarize: func(ctx context.Context, path string) error {
		calls++
		if path != "/fixture" {
			t.Fatal(path)
		}
		return nil
	}})
	m = key(m, '2')
	m = key(m, 's')
	if calls != 0 {
		t.Fatal("background summary")
	}
	if !strings.Contains(m.View().Content, "S summarize") {
		t.Fatal(m.View().Content)
	}
	m = key(m, tea.KeyRight)
	m = key(m, 'j')
	if row, ok := m.(*app).selected(); !ok || row.cwd != "/linked" {
		t.Fatal("worktree selection", row)
	}
	next, cmd := press(m, 'S')
	if calls != 0 || cmd == nil {
		t.Fatal("summary must run in a command", calls)
	}
	m = execute(t, next, cmd)
	if calls != 1 || !strings.Contains(m.View().Content, "queued") {
		t.Fatal(calls, m.View().Content)
	}
	local := m.(*app)
	local.snap.Projects.Rows[0].MachineID = "remote"
	local.rebuild()
	_, cmd = press(local, 'S')
	if cmd != nil || calls != 1 {
		t.Fatal("remote project triggered summary")
	}
}
