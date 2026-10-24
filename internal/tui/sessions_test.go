package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestFlowInitialAndKeyZero(t *testing.T) {
	m := setup(t, fixture(), Options{})
	if m.(*app).tab != flowTab || !strings.Contains(m.View().Content, "0 Flow") {
		t.Fatal(m.View().Content)
	}
	m = key(m, '1')
	m = key(m, '0')
	if m.(*app).tab != flowTab {
		t.Fatal("zero must open Flow")
	}
}

func TestAgentsSplitThresholdAndCursor(t *testing.T) {
	s := fixture()
	s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: 2, Label: "working", Title: "second"})
	s.snap.AgentDetails[2] = view.AgentView{Session: model.Session{ID: 2, Evidence: "second evidence"}}
	for _, width := range []int{139, 140, 160} {
		m := setup(t, s, Options{})
		m = key(key(m, '0'), 's')
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

func TestAgentDetailUsesStateColors(t *testing.T) {
	s := fixture()
	d := s.snap.AgentDetails[1]
	d.Label = "stalled"
	s.snap.AgentDetails[1] = d
	m := setup(t, s, Options{}).(*app)
	m.tab = 0
	m.rebuild()
	m.opts.NoColor = false
	key(m, tea.KeyEnter)
	if !strings.Contains(m.View().Content, view.StateStyle("stalled", false).Render("stalled")) {
		t.Fatalf("detail state lacks red: %q", m.View().Content)
	}
}

func TestNOColorEmptyMeansUnset(t *testing.T) {
	for _, value := range []string{"", "0", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("NO_COLOR", value)
			m := New(&fakeSource{}, Options{}).(*app)
			if m.opts.NoColor != (value != "") {
				t.Fatalf("NO_COLOR=%q disables color: %v", value, m.opts.NoColor)
			}
		})
	}
}

func TestEventDrivenHelpReportsLastEvent(t *testing.T) {
	s := fixture()
	at := s.snap.ObservedAt.Add(-24 * time.Hour)
	s.snap.Freshness = map[string]view.Fresh{"otlp": {LastOKAt: at, Status: "last_event"}, "sockets": {LastOKAt: at, Status: "last_event"}}
	m := setup(t, s, Options{}).(*app)
	m.tab = 0
	m.rebuild()
	key(m, '?')
	if strings.Contains(m.footer(), "last event") || !strings.Contains(m.header(), "intake · receiving") || !strings.Contains(m.View().Content, "last event") || strings.Contains(m.View().Content, "stale") {
		t.Fatal(m.View().Content)
	}
}
