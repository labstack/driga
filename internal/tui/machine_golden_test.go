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
				src.snap.Machine.Sample.TS = src.snap.ObservedAt
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
				if got != string(want) {
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
