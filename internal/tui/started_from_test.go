package tui

import (
	"strings"
	"testing"
)

func TestStartedFromAgentsDetailAndFlow(t *testing.T) {
	for _, surface := range []string{"", "Editor"} {
		s := fixture()
		s.snap.Agents.Rows[0].StartedFrom = surface
		d := s.snap.AgentDetails[1]
		d.Session.StartedFrom = surface
		m := setup(t, s, Options{NoColor: true}).(*app)
		m.width = 140
		m.tab = 0
		m.rebuild()
		rows := m.rows
		found := false
		for _, r := range rows {
			if strings.Contains(strings.Join(r.cells, " "), " · from Editor") {
				found = true
			}
		}
		if found != (surface != "") {
			t.Fatalf("agents: %+v", rows)
		}
		if strings.Contains(agentDetail(d, 140), " · from Editor") != (surface != "") {
			t.Fatal("detail missing surface")
		}
		m.snap.BoardData.Nodes[0].Title = "Task"
		m.snap.BoardData.Nodes[0].StartedFrom = surface
		if strings.Contains(m.flowView(), "Task · from Editor") != (surface != "") {
			t.Fatal("Flow label/detail missing surface", m.flowView())
		}
	}
}
