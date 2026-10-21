package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"strings"
	"testing"
	"time"
)

func fleetSource() *fakeSource {
	s := fixture()
	s.snap.Summary.MachineID = "local"
	s.snap.Machines = view.MachinesView{Rows: []view.MachineRow{{Machine: model.Machine{MachineID: "local", Name: "laptop", State: "local"}}, {Machine: model.Machine{MachineID: "remote", Name: "vm-1", State: "stale", LastContactAt: s.snap.ObservedAt.Add(-12 * time.Minute)}, LastSeenAge: 12 * time.Minute}}}
	s.snap.Agents.Rows[0].MachineID = "local"
	s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{MachineID: "remote", ID: 2, Adapter: "codex", Label: "working", Title: "remote title"})
	return s
}
func TestTUIHeaderAndMachineColumn(t *testing.T) {
	m := setup(t, fleetSource(), Options{})
	m = key(m, '1')
	text := m.View().Content
	for _, want := range []string{"+1", "vm-1 stale 12m", "MACHINE"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("wide: %q", line)
		}
	}
	m = key(m, 'm')
	if strings.Contains(m.View().Content, "remote title") {
		t.Fatal("local scope contains remote row")
	}
	m = key(m, 'm')
	if strings.Contains(m.View().Content, "test title") {
		t.Fatal("remote scope contains local row")
	}
}
func TestStaleRowsDimmed(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := setup(t, fleetSource(), Options{}).(*app)
	key(m, '1')
	m.opts.NoColor = false
	m.rebuild()
	text := m.View().Content
	if !strings.Contains(ansi.Strip(text), "stale 12m") || !strings.Contains(text, "\x1b[2m") {
		t.Fatalf("stale not dimmed: %q", text)
	}
}
func TestTUIKillRefusesRemoteOrphan(t *testing.T) {
	s := fleetSource()
	s.snap.Orphans.Rows[0].MachineID = "remote"
	s.snap.Orphans.Rows[0].Orphan.MachineID = "remote"
	calls := 0
	m := setup(t, s, Options{Spool: func(model.HookEvent) error { calls++; return nil }})
	m = key(m, '4')
	m = key(m, 'k')
	if !strings.Contains(m.View().Content, "orphan is on vm-1; run driga orphans kill there") || m.(*app).confirm != nil || calls != 0 {
		t.Fatal(m.View().Content)
	}
}

func TestTUIMachineScopeCountsAndHealth(t *testing.T) {
	s := fleetSource()
	s.snap.Summary.Agents = 2
	s.snap.Machine = view.MachineView{MachineID: "local", Sample: model.MachineSample{MemUsed: 100 << 20, MemTotal: 200 << 20}, Machines: []view.MachineView{{MachineID: "local", Sample: model.MachineSample{MemUsed: 100 << 20, MemTotal: 200 << 20}}, {MachineID: "remote", Sample: model.MachineSample{MemUsed: 300 << 20, MemTotal: 400 << 20}}}}
	m := setup(t, s, Options{})
	m = key(m, 'm')
	if !strings.Contains(m.View().Content, "1 Agents 1") {
		t.Fatal(m.View().Content)
	}
	m = key(m, 'm')
	m = key(m, '5')
	if !strings.Contains(m.View().Content, "300.0M/400.0M") || strings.Contains(m.View().Content, "100.0M/200.0M") {
		t.Fatal(m.View().Content)
	}
}

func TestPeerFreshnessUsesPeerThreshold(t *testing.T) {
	s := fleetSource()
	now := s.snap.ObservedAt
	s.snap.Machines.Rows[1].State = "live"
	s.snap.Machines.Rows[1].LastContactAt = now.Add(-20 * time.Second)
	s.snap.Freshness = map[string]view.Fresh{"remote": {LastOKAt: now.Add(-20 * time.Second)}, "remote/git": {LastOKAt: now.Add(-time.Minute)}}
	m := setup(t, s, Options{}).(*app)
	if strings.Contains(m.footer(), "stale remote") {
		t.Fatal(m.footer())
	}
}
