package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"strings"
	"testing"
	"time"
)

func TestFlowAttentionJumpAndCopy(t *testing.T) {
	for _, remote := range []bool{false, true} {
		calls := 0
		m := setup(t, fixture(), Options{Tmux: "/tmp/fixture,1,0", Exec: func(args []string) error {
			calls++
			if args[0] != "tmux" {
				t.Fatal(args)
			}
			return nil
		}, Now: func() time.Time { return flowtest.Now }}).(*app)
		m.flow = flowFixture()
		m.tab = flowTab
		m.width = 100
		m.loaded = true
		m.snap.LocalMachineID = "local"
		machine := "local"
		if remote {
			machine = "peer"
		}
		m.flow.Children[0].MachineID = machine
		m.flow.Attention.Waits = []view.InboxRow{{MachineID: machine, Title: "Need approval", InboxItem: model.InboxItem{MachineID: machine, SessionID: 2, Kind: "waiting", TS: flowtest.Now.Add(-time.Minute), Jump: &model.InboxJump{Kind: "tmux", Server: "/tmp/fixture", Target: "dev:1.2", Command: "tmux attach -t 'dev:1.2'"}}}}
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if !m.flowAttention {
			t.Fatal("tab did not focus attention")
		}
		if !strings.Contains(m.flowView(), "Copy resume: tmux attach") {
			t.Fatal("resume not in details")
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd != nil {
			cmd()
		}
		if !remote && calls != 2 || remote && calls != 0 {
			t.Fatal("unexpected jump execution", remote, calls)
		}
		if m.flowSelected().ID != 2 {
			t.Fatal("jump did not select map row")
		}
	}
}
func TestFlowZoomKeys(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = flowTab
	for _, tc := range []struct {
		key     rune
		minutes int
	}{{'-', 240}, {'+', 60}, {'+', 15}, {'+', 15}, {'-', 60}} {
		m.Update(tea.KeyPressMsg{Code: tc.key, Text: string(tc.key)})
		if m.flowZoom() != tc.minutes {
			t.Fatal(tc, m.flowZoom())
		}
	}
}
