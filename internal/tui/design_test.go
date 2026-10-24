package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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

func TestThreeTabsOnEveryDestination(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.width = 160
	for _, tab := range []int{flowTab, 0, 1, 2, 3, 4, 5, chatTab} {
		m.tab = tab
		bar := m.tabBar()
		for _, name := range []string{"0 Flow", "1 Projects", "2 Machines"} {
			if !strings.Contains(bar, name) {
				t.Fatal(tab, bar)
			}
		}
		for _, name := range []string{"Home", "Agents", "Inbox", "Orphans", "Cleanup", "Chat"} {
			if strings.Contains(bar, name) {
				t.Fatal("removed tab", bar)
			}
		}
	}
}

func TestMachineScopeKeepsThreeDestinations(t *testing.T) {
	m := setup(t, fleetSource(), Options{}).(*app)
	m.width = 160
	before := m.tabBar()
	m.machineScope = "remote"
	if m.tabBar() != before {
		t.Fatal("scope removed navigation")
	}
}
