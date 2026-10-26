package tui

import (
	"fmt"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"strings"
	"testing"
)

func TestHeaderShowsCPUAndDiskTotals(t *testing.T) {
	m := boardApp(t, "quiet")
	m.width = 250
	cpu := 37.0
	m.snap.Summary.Sample = model.MachineSample{TS: m.opts.Now(), CPUPercent: &cpu, MemUsed: 50, MemTotal: 100, DiskTotal: 500, DiskFree: 100, Load1: 9, Cores: 4}
	m.snap.Machine.Machines = nil
	got := m.header()
	if !strings.Contains(got, "CPU 37%") || strings.Contains(got, "load ") || !strings.Contains(got, "disk "+view.HumanBytes(400)+"/"+view.HumanBytes(500)) {
		t.Fatal(got)
	}
	m.snap.Summary.Sample.CPUPercent = nil
	m.snap.Summary.Sample.DiskTotal = 0
	got = m.header()
	if !strings.Contains(got, "load 9.0/4") || !strings.Contains(got, "disk free "+view.HumanBytes(100)) {
		t.Fatal(got)
	}
}

func TestFlowNeedYouMatchesBoardWaitingSessions(t *testing.T) {
	m := boardApp(t, "busy")
	m.width = 200
	m.snap.LocalMachineID = "local"
	m.snap.Agents.Rows = nil
	m.snap.Inbox.Rows = nil
	for _, r := range m.board.Needs {
		m.snap.Agents.Rows = append(m.snap.Agents.Rows, view.AgentRow{MachineID: r.Node.MachineID, ID: r.Node.ID, Label: "waiting"})
	}
	if len(m.board.Needs) == 0 {
		t.Fatal("fixture has no waiting sessions")
	}
	// A live approval for a session already waiting must not add another need.
	r := m.board.Needs[0]
	m.snap.LocalMachineID = r.Node.MachineID
	m.prompts = []chat.Prompt{{SessionID: r.Node.ID}}
	got, _ := m.boardLayout()
	if want := fmt.Sprintf("Need you %d", len(m.board.Needs)); !strings.Contains(got, want) {
		t.Fatalf("missing %q: %s", want, got)
	}
}
func TestFlowPulseUsesSharedAttentionAndHygiene(t *testing.T) {
	m := boardApp(t, "quiet")
	m.width = 200
	m.snap.LocalMachineID = "local"
	m.snap.Inbox.Rows = []view.InboxRow{{MachineID: "local", InboxItem: model.InboxItem{SessionID: 1, Kind: "waiting"}}, {MachineID: "local", InboxItem: model.InboxItem{SessionID: 2, Kind: "finished"}}, {MachineID: "local", InboxItem: model.InboxItem{SessionID: 2, Kind: "finished"}}}
	m.prompts = []chat.Prompt{{SessionID: 1}, {SessionID: 3}}
	m.snap.Agents.Rows = []view.AgentRow{{Label: "~stuck"}}
	m.snap.Orphans.Rows = []view.OrphanRow{{}}
	m.snap.Summary.Reclaim = 70
	m.snap.Summary.Sample.MemUsed = 50
	m.snap.Summary.Sample.MemTotal = 100
	got, _ := m.boardLayout()
	for _, want := range []string{"Need you 2", "To review 1", "Stuck 1", "Leftover 1", "Reclaimable " + view.HumanBytes(70), "Memory " + view.HumanBytes(50) + "/" + view.HumanBytes(100)} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}
