package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestMachineGoldenFrames(t *testing.T) {
	// The header clock renders local time; pin the zone so CI in UTC matches.
	original := time.Local
	time.Local = time.FixedZone("fixture local", -7*60*60)
	t.Cleanup(func() { time.Local = original })
	for _, width := range []int{80, 140} {
		for _, expanded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d-expanded-%t", width, expanded), func(t *testing.T) {
				src := fixture()
				sampleApp := view.MachineApp{ID: "/Applications/Browser.app", Name: "Browser", Memory: 2 << 30, CPUPercent: 70, MemoryMeasure: "physical_footprint", Processes: []view.MachineProcess{{Proc: model.Proc{PID: 42, Exe: "/bin/worker", MemoryMeasure: "physical_footprint", CPUPercent: 70}, Memory: 2 << 30, Attribution: &view.MachineAttribution{SessionID: 251, Project: "fixture", Proof: "parent_chain"}}}}
				src.snap.Machine = view.MachineView{Sample: model.MachineSample{PressureLevel: "critical", MemUsed: 4 << 30, MemCompressed: 1 << 30, MemTotal: 8 << 30, SwapUsed: 1 << 30, SwapTotal: 2 << 30, DiskFree: 12 << 30, Cores: 4}, TopMemory: []view.MachineApp{sampleApp}, TopCPU: []view.MachineApp{sampleApp}}
				src.snap.Machine.AgentShare = view.AgentShare{Memory: 2 << 30, MachineMemory: 4 << 30, MemoryMeasure: "physical_footprint", CPUPercent: 70, Processes: 1, MCPServers: 3}
				src.snap.Machine.SessionCosts = []view.SessionCost{{SessionID: 251, Adapter: "claude", Project: "fixture", StartedFrom: "Editor", Memory: 2 << 30, CPUPercent: 70, Processes: sampleApp.Processes}}
				src.snap.Machine.OtherApps = view.MachineApp{ID: "other", Name: "Other apps", Memory: 1 << 30, MemoryMeasure: "rss"}
				src.snap.Machine.Sample.TS = src.snap.ObservedAt
				at := src.snap.ObservedAt
				ps := []model.Proc{
					{PID: 10, Cmdline: "codex app-server", Argv: []string{"codex", "app-server"}, StartTime: at.Add(-31 * time.Hour)},
					{PID: 11, PPID: 10, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, RSS: 100 << 20, StartTime: at.Add(-160 * time.Minute)},
					{PID: 12, PPID: 10, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, RSS: 100 << 20, StartTime: at},
					{PID: 13, PPID: 10, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, RSS: 100 << 20, StartTime: at.Add(-80 * time.Minute)},
				}
				trends := derive.NewMCPTrendBuilder()
				for i, count := range []int{2, 3, 4} {
					// PID 12 starts last, so keep chronological membership here.
					series := []model.Proc{ps[0], ps[1], ps[3], ps[2]}
					v := derive.MCPServers(series[:count], nil, at.Add(time.Duration(i-2)*80*time.Minute))
					for _, s := range v.Samples {
						trends.Add(s)
					}
				}
				src.snap.Machine.MCPServers = derive.MCPServers(ps, nil, at)
				derive.ApplyMCPTrends(&src.snap.Machine.MCPServers, trends.Trends())
				src.snap.Summary.Sample = src.snap.Machine.Sample
				m := setup(t, src, Options{InitialTab: "machine"}).(*app)
				if m.tab != 4 {
					t.Fatal("initial tab ignored")
				}
				m.width, m.height = width, 50
				m.rebuild()
				first := -1
				for i, r := range m.rows {
					if r.app != nil {
						first = i
						break
					}
				}
				if first < 0 {
					t.Fatal("no app rows")
				}
				m.cursors[4] = first
				if expanded {
					m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
					if !strings.Contains(strings.Join(m.rows[first+1].cells, " "), "session #251") {
						t.Fatal("expand missing attribution")
					}
				}
				m.rebuild()
				got := m.View().Content + "\n"
				lines := strings.Split(got, "\n")
				for i := range lines {
					lines[i] = strings.TrimRight(lines[i], " ")
				}
				got = strings.Join(lines, "\n")
				for _, line := range strings.Split(got, "\n") {
					if lipgloss.Width(line) > width {
						t.Fatal("overflow", line)
					}
				}
				name := filepath.Join("testdata", fmt.Sprintf("machine-%d-expanded-%t.golden", width, expanded))
				if os.Getenv("UPDATE_MACHINE_GOLDEN") == "1" {
					if err := os.WriteFile(name, []byte(got), 0644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
					t.Fatal("golden drift", name)
				}
				m.openDetail(m.rows[first])
				if !strings.Contains(m.detailText, "proof: parent_chain") || !strings.Contains(m.detailText, "physical_footprint") {
					t.Fatal(m.detailText)
				}
			})
		}
	}
}

func TestMachineWarningJumpStaysInCurrentTUI(t *testing.T) {
	m := setup(t, fixture(), Options{Exec: func([]string) error { t.Fatal("launched terminal"); return nil }}).(*app)
	m.tab = 2
	item := model.InboxItem{ID: "low_disk:1", Kind: "low_disk", Jump: &model.InboxJump{Kind: "machine", Command: "driga top --tab machine"}}
	if cmd := m.jumpInbox(tabRow{inbox: &item}); cmd != nil {
		t.Fatal("external command for internal jump")
	}
	if m.tab != 4 {
		t.Fatal("did not switch to Machine")
	}
}

func TestMachineViewOlderPeerFields(t *testing.T) {
	src := fixture()
	src.snap.Machine = view.MachineView{Machines: []view.MachineView{{MachineID: "peer", TopMemory: []view.MachineApp{{ID: "legacy", Name: "Legacy", Memory: 2 << 30}}}}}
	m := setup(t, src, Options{InitialTab: "machine"}).(*app)
	m.rebuild()
	var text strings.Builder
	otherKey := ""
	for _, r := range m.rows {
		line := strings.Join(r.cells, " ")
		text.WriteString(line)
		if strings.Contains(line, "Other apps") {
			otherKey = r.key
		}
	}
	if !strings.Contains(text.String(), "Other apps") || !strings.Contains(text.String(), "2.0G") {
		t.Fatal(text.String())
	}
	if strings.Contains(text.String(), "Legacy") {
		t.Fatal("Other apps started expanded")
	}
	m.expanded[otherKey] = true
	m.rebuild()
	text.Reset()
	for _, r := range m.rows {
		text.WriteString(strings.Join(r.cells, " "))
	}
	if !strings.Contains(text.String(), "Legacy") {
		t.Fatal(text.String())
	}
}
