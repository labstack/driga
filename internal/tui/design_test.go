package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestHeaderGaugeLevels(t *testing.T) {
	for _, tc := range []struct {
		used  uint64
		level string
	}{{20, "normal"}, {85, "warning"}, {98, "critical"}} {
		if view.GaugeLevel(tc.used, 100) != tc.level {
			t.Fatal(tc)
		}
		if got := headerGauge("mem", tc.used, 100, true); !strings.Contains(got, "%") || lipgloss.Width(got) > 22 {
			t.Fatal(got)
		}
		if got := headerGauge("mem", tc.used, 100, false); !strings.Contains(got, view.GaugeStyle(tc.used, 100, false).Render(strings.Repeat("█", int(tc.used)*6/100))) {
			t.Fatal("header gauge does not use level color", got)
		}
	}
	s := fixture()
	s.snap.Summary.Sample = model.MachineSample{TS: s.snap.ObservedAt, MemUsed: 85, MemTotal: 100, SwapUsed: 98, SwapTotal: 100, Load1: 16.9, Cores: 14, DiskFree: 100 << 30}
	m := setup(t, s, Options{}).(*app)
	m.width = 160
	for _, want := range []string{"mem", "swap", "load", "disk", "85%", "98%"} {
		if !strings.Contains(m.header(), want) {
			t.Errorf("header missing %s: %s", want, m.header())
		}
	}
}

func TestTabCountsAndInboxColor(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.width = 160
	for _, want := range []string{"1 Agents 1", "2 Projects", "3 Inbox 1", "4 Orphans", "6 Cleanup 0"} {
		if !strings.Contains(m.tabBar(), want) {
			t.Errorf("missing %q: %s", want, m.tabBar())
		}
	}
	m.opts.NoColor = false
	if !strings.Contains(m.tabBar(), view.StateStyle("waiting", false).Render("1")) {
		t.Fatal("Inbox count lacks amber color")
	}
}

func TestHomeDesignWidthsAndSourceStatus(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, width := range []int{80, 120, 160} {
			s := fixture()
			s.snap.Freshness = map[string]view.Fresh{"spool": {LastOKAt: s.snap.ObservedAt}, "otlp": {Status: "not-yet-receiving"}}
			s.snap.Machine.Sample = model.MachineSample{TS: s.snap.ObservedAt, MemUsed: 85, MemTotal: 100, SwapUsed: 98, SwapTotal: 100, Load1: 16.9, Cores: 14}
			s.snap.Summary.Sample = s.snap.Machine.Sample
			s.snap.Activity.Machines = []view.ActivityMachine{{Buckets: make([]view.ActivityBucket, 60)}}
			s.snap.Activity.Machines[0].Buckets[59] = view.ActivityBucket{Sample: &s.snap.Machine.Sample, Events: 8}
			m := setup(t, s, Options{}).(*app)
			m.opts.NoColor = noColor
			m.Update(tea.WindowSizeMsg{Width: width, Height: 44})
			for _, text := range []string{m.header(), m.tabBar(), m.homeView(), m.View().Content} {
				for _, line := range strings.Split(text, "\n") {
					if lipgloss.Width(line) > width {
						t.Fatalf("width=%d noColor=%v: %q", width, noColor, line)
					}
				}
				if noColor && strings.Contains(text, "\x1b") {
					t.Fatal("NO_COLOR emitted ANSI")
				}
			}
			for _, want := range []string{"hooks", "telemetry", "waiting for first event", "−60m", "now", "14 cores"} {
				if !strings.Contains(m.homeView(), want) {
					t.Errorf("width %d missing %s", width, want)
				}
			}
			if width == 160 {
				for _, want := range []string{"Needs you", "Agents", "Machine", "Activity", "Projects", "Leftovers"} {
					if !strings.Contains(m.View().Content, want) {
						t.Errorf("160x44 missing panel %s", want)
					}
				}
			}
		}
	}
}

func TestTabCleanupCountFollowsMachineScope(t *testing.T) {
	s := fleetSource()
	s.snap.LocalMachineID = "local"
	s.snap.Cleanup = []cleanup.Candidate{{ID: "one"}, {ID: "two"}}
	m := setup(t, s, Options{}).(*app)
	m.width = 160
	if !strings.Contains(m.tabBar(), "6 Cleanup 2") {
		t.Fatal(m.tabBar())
	}
	m.machineScope = "remote"
	if !strings.Contains(m.tabBar(), "6 Cleanup 0") {
		t.Fatal(m.tabBar())
	}
}
