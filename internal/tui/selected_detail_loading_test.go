package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type selectionSource struct {
	*fakeSource
	calls        []int64
	projectCalls []int64
}

func (s *selectionSource) LoadAgent(_ context.Context, _ store.Scope, id int64) (view.AgentView, error) {
	s.calls = append(s.calls, id)
	return view.AgentView{Session: model.Session{ID: id, Evidence: "selected evidence"}}, nil
}
func (s *selectionSource) LoadProject(_ context.Context, _ store.Scope, id int64, _ bool) (view.WhereView, error) {
	s.projectCalls = append(s.projectCalls, id)
	return view.WhereView{ProjectRow: view.ProjectRow{Status: view.ProjectStatus{Repo: model.Repo{ID: id, Name: "selected project detail"}}}}, nil
}
func TestTUILoadsOnlySelectedSessionDetail(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	src := &selectionSource{fakeSource: fixture()}
	m.src = src
	m.snap.AgentDetails = nil
	m.tab = 0
	m.rebuild()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selection did not request detail")
	}
	execute(t, m, cmd)
	if len(src.calls) != 1 || src.calls[0] != 1 || len(m.snap.AgentDetails) != 1 || !strings.Contains(m.View().Content, "selected evidence") {
		t.Fatal(src.calls, m.View().Content)
	}
	if _, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("unchanged selection reloaded detail")
	}
}

func TestTUILoadsOnlySelectedProjectDetail(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	src := &selectionSource{fakeSource: fixture()}
	m.src = src
	m.snap.ProjectDetails = nil
	m.snap.Projects.Rows = []view.ProjectRow{{Status: view.ProjectStatus{Repo: model.Repo{ID: 1, Name: "one"}}}, {Status: view.ProjectStatus{Repo: model.Repo{ID: 2, Name: "two"}}}}
	m.tab = 1
	m.rebuild()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selection did not request project detail")
	}
	execute(t, m, cmd)
	if len(src.projectCalls) != 1 || src.projectCalls[0] != 1 || len(m.snap.ProjectDetails) != 1 || !strings.Contains(m.detailText, "selected project detail") {
		t.Fatal(src.projectCalls, m.detailText)
	}
	if _, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("unchanged project selection reloaded")
	}
}
