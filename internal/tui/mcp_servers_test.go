package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/view"
)

func TestMCPRollupRowAndDetails(t *testing.T) {
	src := fixture()
	ps := []model.Proc{{PID: 1, Cmdline: "claude", Argv: []string{"claude"}}, {PID: 2, PPID: 1, Cmdline: "node exec firecrawl-mcp", Argv: []string{"node", "exec", "firecrawl-mcp"}, RSS: 10 << 20}, {PID: 3, PPID: 2, Cmdline: "node /cache/node_modules/.bin/firecrawl-mcp", Argv: []string{"node", "/cache/node_modules/.bin/firecrawl-mcp"}, RSS: 20 << 20}}
	src.snap.Machine = view.MachineView{MCPServers: derive.MCPServers(ps, nil, src.snap.ObservedAt)}
	for _, width := range []int{80, 140} {
		m := setup(t, src, Options{InitialTab: "machine"}).(*app)
		m.width, m.height = width, 50
		m.rebuild()
		if !strings.Contains(m.View().Content, "firecrawl-mcp  1 servers · 30.0M · 1 hosts · 0 duplicates") {
			t.Fatal(m.View().Content)
		}
		found := false
		for _, r := range m.rows {
			if len(r.cells) > 1 && r.cells[1] == "firecrawl-mcp" {
				found = true
				m.openDetail(r)
				for _, want := range []string{"includes launcher", "Installs:", ps[1].Cmdline, ps[2].Cmdline} {
					if !strings.Contains(m.detailText, want) {
						t.Errorf("missing %q: %s", want, m.detailText)
					}
				}
			}
		}
		if !found {
			t.Fatal("missing roll-up row")
		}
	}
}

func TestMachineMCPServersReadOnlyDetails(t *testing.T) {
	src := fixture()
	sigs, _ := signatures.Load(nil)
	at := src.snap.ObservedAt
	servers := derive.MCPServers([]model.Proc{{PID: 10, Cmdline: "codex app-server", Argv: []string{"codex", "app-server"}, StartTime: at.Add(-31 * time.Hour)}, {PID: 11, PPID: 10, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, RSS: 100, StartTime: at.Add(-time.Hour)}, {PID: 12, PPID: 10, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, RSS: 100, StartTime: at}}, sigs, at)
	src.snap.Machine = view.MachineView{MCPServers: servers}
	for _, width := range []int{80, 140} {
		m := setup(t, src, Options{InitialTab: "machine", Exec: func([]string) error { t.Fatal("executed command"); return nil }, Kill: func(context.Context, model.Orphan, []int32, bool) (kill.Result, error) {
			t.Fatal("attempted to signal pile-up")
			return kill.Result{}, nil
		}}).(*app)
		m.width, m.height = width, 50
		m.rebuild()
		found := false
		for i, r := range m.rows {
			if r.pileup != nil {
				found = true
				m.cursors[4] = i
				for _, code := range []rune{'k', 'K', 'y'} {
					_, cmd := press(m, code)
					if cmd != nil {
						cmd()
					}
				}
				m.openDetail(r)
				for _, want := range []string{"codegraph mcp", "2 processes", "codex app-server", "PID 10", "31h", "Oldest", "Newest", "Restart", "per project"} {
					if !strings.Contains(m.detailText, want) {
						t.Fatalf("missing %q: %s", want, m.detailText)
					}
				}
			}
		}
		if !found {
			t.Fatal("no pile-up row")
		}
		for _, line := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatal("overflow", line)
			}
		}
	}
}
