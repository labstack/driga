package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestHomeInitialAndKeyZero(t *testing.T) {
	m := setup(t, fixture(), Options{})
	if !strings.Contains(m.View().Content, "[0 Home]") {
		t.Fatal(m.View().Content)
	}
	m = key(m, '1')
	m = key(m, '0')
	if !strings.Contains(m.View().Content, "[0 Home]") {
		t.Fatal(m.View().Content)
	}
	m = key(m, '?')
	if !strings.Contains(m.View().Content, "0 Home") {
		t.Fatal(m.View().Content)
	}
}

func TestHomeWidths(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		s := fixture()
		s.snap.Machine.Sample = model.MachineSample{TS: s.snap.ObservedAt, MemUsed: 40 << 20, MemTotal: 80 << 20, SwapUsed: 1 << 20, SwapTotal: 4 << 20, Load1: 2, Cores: 4, DiskFree: 80 << 30}
		s.snap.Activity.Machines = []view.ActivityMachine{{Buckets: make([]view.ActivityBucket, 60)}}
		s.snap.Activity.Machines[0].Buckets[59] = view.ActivityBucket{Sample: &s.snap.Machine.Sample, Events: 3}
		m := setup(t, s, Options{})
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 100})
		text := m.View().Content
		for _, title := range []string{"Needs you", "Agent states", "Machine", "Activity", "Projects", "Leftovers"} {
			if !strings.Contains(text, title) {
				t.Errorf("width %d missing %s", width, title)
			}
		}
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: %q", width, line)
			}
		}
		if strings.Contains(text, "\x1b") {
			t.Fatal("ANSI with NoColor")
		}
		if !strings.Contains(text, "····") {
			t.Fatal("sample gaps lost", text)
		}
	}
}

func TestHomeWaitingAndStalledWithoutInbox(t *testing.T) {
	s := fixture()
	s.snap.Inbox.Rows = nil
	s.snap.Agents.Rows[0].Label = "waiting"
	s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: 2, Label: "~stalled?", Title: "stalled agent"})
	s.snap.AgentDetails[2] = view.AgentView{Session: model.Session{ID: 2, SessionObs: model.SessionObs{LastActivityAt: s.snap.ObservedAt}}}
	m := setup(t, s, Options{Spool: func(model.HookEvent) error { t.Fatal("invented ack event"); return nil }}).(*app)
	if len(m.rows) != 2 || m.rows[0].agent != 2 {
		t.Fatalf("rows: %+v", m.rows)
	}
	_, cmd := press(m, 'x')
	if cmd != nil || !strings.Contains(m.status, "no inbox event") {
		t.Fatal(m.status)
	}
}

func TestHomeNeedsYouNewestFirstAndActions(t *testing.T) {
	s := fixture()
	now := s.snap.ObservedAt
	s.snap.Inbox.Rows = []view.InboxRow{
		{InboxItem: model.InboxItem{ID: "old", SessionID: 1, Kind: "finished", TS: now.Add(-time.Hour), Summary: "old completion"}},
		{InboxItem: model.InboxItem{ID: "new", SessionID: 1, Kind: "waiting", TS: now, Summary: "new attention"}},
		{InboxItem: model.InboxItem{ID: "acked", Kind: "finished", TS: now, AckedAt: &now}},
	}
	var ack string
	m := setup(t, s, Options{Spool: func(e model.HookEvent) error { ack = e.Message; return nil }})
	a := m.(*app)
	if len(a.rows) != 2 || a.rows[0].inbox.ID != "new" || a.rows[1].inbox.ID != "old" {
		t.Fatalf("rows: %+v", a.rows)
	}
	m = key(m, tea.KeyEnter)
	if ack != "" || !strings.Contains(m.View().Content, "hook notification") {
		t.Fatal(m.View().Content, ack)
	}
	m, cmd := press(m, 'x')
	execute(t, m, cmd)
	if ack != "new" {
		t.Fatal(ack)
	}
}

func TestAgentsSplitThresholdAndCursor(t *testing.T) {
	s := fixture()
	s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: 2, Label: "working", Title: "second"})
	s.snap.AgentDetails[2] = view.AgentView{Session: model.Session{ID: 2, Evidence: "second evidence"}}
	for _, width := range []int{139, 140, 160} {
		m := setup(t, s, Options{})
		m = key(m, '1')
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 60})
		if strings.Contains(m.View().Content, "Evidence:") != (width >= 140) {
			t.Fatalf("width %d: %s", width, m.View().Content)
		}
		if width >= 140 {
			m = key(m, 'j')
			if !strings.Contains(m.View().Content, "second evidence") {
				t.Fatal(m.View().Content)
			}
		} else {
			m = key(m, tea.KeyEnter)
			if !strings.Contains(m.View().Content, "hook notification") {
				t.Fatal(m.View().Content)
			}
			m = key(m, tea.KeyEscape)
			if strings.Contains(m.View().Content, "Evidence:") {
				t.Fatal(m.View().Content)
			}
		}
	}
}

func TestHomePanelsFitWithoutClipping(t *testing.T) {
	s := fixture()
	s.snap.Agents.Rows[0].Title = strings.Repeat("界é", 100)
	s.snap.Inbox.Rows[0].Summary = strings.Repeat("界é", 100)
	for _, width := range []int{80, 109, 110, 120, 139, 140, 149, 150, 160} {
		m := setup(t, s, Options{}).(*app)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 100})
		for _, line := range strings.Split(m.homeView(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: %q", width, line)
			}
			if strings.Contains(line, "working") && !strings.Contains(line, "█") {
				t.Fatalf("state bar wrapped: %q", line)
			}
		}
		if strings.Count(strings.Split(m.homeView(), "\n")[0], "┌") != homeColumns(width) {
			t.Fatal("wrong column count", width)
		}
		if strings.Contains(bar(1, 1, 20, lipgloss.NewStyle()), "\n") {
			t.Fatal("bar wraps onto another line")
		}
	}
}

func TestAgentDetailUsesStateColors(t *testing.T) {
	s := fixture()
	d := s.snap.AgentDetails[1]
	d.Label = "stalled?"
	s.snap.AgentDetails[1] = d
	m := setup(t, s, Options{}).(*app)
	m.opts.NoColor = false
	key(m, tea.KeyEnter)
	if !strings.Contains(m.View().Content, "\x1b[31m") {
		t.Fatalf("detail state lacks red: %q", m.View().Content)
	}
}

func TestHomeHonorsNO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := New(fixture(), Options{DataDir: t.TempDir()}).(*app)
	m.Update(refreshMsg{snap: fixture().snap, changed: true})
	if !m.opts.NoColor || strings.Contains(m.View().Content, "\x1b") {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestHomeFleetAttentionKeepsFields(t *testing.T) {
	s := fleetSource()
	s.snap.Inbox.Rows[0].MachineID = "local"
	s.snap.Inbox.Rows = append(s.snap.Inbox.Rows, view.InboxRow{MachineID: "remote", Project: "peer-project", InboxItem: model.InboxItem{ID: "peer", SessionID: 2, Kind: "waiting", Summary: "peer needs input", TS: s.snap.ObservedAt}})
	m := setup(t, s, Options{}).(*app)
	if len(m.rows) != 2 || m.rows[0].cells[1] != "waiting" || m.rows[0].cells[2] != "peer-project" {
		t.Fatalf("attention fields shifted: %+v", m.rows)
	}
	if !strings.Contains(m.View().Content, "peer needs input") {
		t.Fatal(m.View().Content)
	}
}

func TestHomeCurrentAttentionNotHiddenByCompletion(t *testing.T) {
	s := fixture()
	s.snap.Inbox.Rows[0].Kind = "finished"
	s.snap.Agents.Rows[0].Label = "~stalled?"
	m := setup(t, s, Options{}).(*app)
	if len(m.rows) != 2 {
		t.Fatalf("completion hid current stalled session: %+v", m.rows)
	}
}

func TestHomeLoadsLeftoversAndScrolls(t *testing.T) {
	s := fixture()
	m := New(s, Options{DataDir: t.TempDir(), NoColor: true, CleanupList: func(context.Context) ([]cleanup.Candidate, error) {
		return []cleanup.Candidate{{Kind: cleanup.Worktree, Status: "ready"}, {Kind: cleanup.Worktree, Status: "refused"}}, nil
	}})
	m, cmd := m.Update(m.(*app).poll()())
	if cmd != nil {
		m = execute(t, m, cmd)
	}
	if m.(*app).snap.Cleanup == nil {
		t.Fatal("Home did not load leftovers")
	}
	m = key(m, tea.KeyPgDown)
	m = key(m, tea.KeyPgDown)
	if !strings.Contains(m.View().Content, "1 ready · 1 refused") {
		t.Fatal(m.View().Content)
	}
}
