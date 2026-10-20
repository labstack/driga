package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/view"
)

func TestFlowObservedPulseAndSpark(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		for _, noColor := range []bool{false, true} {
			now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
			m := setup(t, fixture(), Options{NoColor: noColor, Now: func() time.Time { return now }}).(*app)
			m.tab = flowTab
			m.loaded = true
			m.Update(tea.WindowSizeMsg{Width: width, Height: 100})
			snapshot := func(at time.Time) view.FlowView {
				f := flowFixture()
				series := make([]int64, 60)
				series[0] = 1
				series[59] = 12288
				f.Lead.Observed = &view.FlowObserved{LogBytesPerMin: series, CPUMSPerMin: make([]int64, 60), LastGrowthAt: at, LastGrowthBytes: 12288}
				f.Children[0].Observed = f.Lead.Observed
				return f
			}
			update := func(at time.Time) tea.Cmd {
				_, cmd := m.Update(flowMsg{id: m.flowID, scope: m.machineScope, flow: snapshot(at)})
				return cmd
			}
			update(now)
			if strings.Contains(m.flowView(), "+12 KB transcript") {
				t.Fatal("history pulsed")
			}
			now = now.Add(time.Second)
			if cmd := update(now); cmd == nil {
				t.Fatal("growth did not schedule animation")
			}
			before := m.flowView()
			if strings.Count(before, "+12 KB transcript") < 2 || !strings.Contains(before, "█") {
				t.Fatalf("width=%d pulse/spark missing: %s", width, before)
			}
			for _, line := range strings.Split(before, "\n") {
				if lipgloss.Width(line) > width || strings.ContainsRune(line, '\t') {
					t.Fatalf("invalid frame %q", line)
				}
			}
			if noColor && strings.Contains(before, "\x1b[") {
				t.Fatal("no-color emitted ANSI")
			}
			// A frame advances the marker one terminal cell without changing data.
			m.Update(flowObservationFrameMsg{})
			if before == m.flowView() {
				t.Fatal("dot did not move")
			}
			now = now.Add(2 * time.Second)
			m.Update(flowObservationFrameMsg{})
			update(now.Add(-2 * time.Second)) // Same stored timestamp after pulse expiry.
			if strings.Contains(m.flowView(), "+12 KB transcript") {
				t.Fatal("unchanged refresh replayed pulse")
			}
			update(now.Add(-3 * time.Second))
			update(now.Add(-2 * time.Second))
			if strings.Contains(m.flowView(), "+12 KB transcript") {
				t.Fatal("out-of-order refresh replayed pulse")
			}
		}
	}
}

func TestFlowObservedLeadWireAt80(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return now }}).(*app)
	m.width = 80
	m.height = 100
	m.tab = flowTab
	m.loaded = true
	load := func(at time.Time) {
		f := flowFixture()
		f.Children = nil
		f.Lead.Observed = &view.FlowObserved{LogBytesPerMin: make([]int64, 60), LastGrowthAt: at, LastGrowthBytes: 10}
		m.Update(flowMsg{id: m.flowID, scope: m.machineScope, flow: f})
	}
	load(now)
	now = now.Add(time.Second)
	load(now)
	text := m.flowView()
	if !strings.Contains(text, "\n  ●\n") {
		t.Fatal("narrow lead wire has no growth marker")
	}
	m.Update(flowObservationFrameMsg{})
	if text == m.flowView() {
		t.Fatal("narrow lead marker did not advance")
	}
}
