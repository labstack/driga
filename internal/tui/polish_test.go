package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestCompactIntakeFooter(t *testing.T) {
	for _, size := range [][2]int{{160, 44}, {120, 40}} {
		for _, state := range []string{"receiving", "waiting", "attention"} {
			s := fixture()
			s.snap.Freshness = map[string]view.Fresh{"sockets": {Status: "last_event", LastOKAt: s.snap.ObservedAt.Add(-227 * time.Minute)}}
			if state == "waiting" {
				s.snap.Freshness["otlp"] = view.Fresh{Status: "not_yet_receiving"}
			}
			if state == "attention" {
				s.snap.Freshness["procs"] = view.Fresh{Stale: true}
			}
			m := setup(t, s, Options{}).(*app)
			m.width, m.height = size[0], size[1]
			m.rebuild()
			for _, notice := range []string{"otlp:", "sockets:", "last event", "stale", "not-yet"} {
				if strings.Contains(m.footer(), notice) {
					t.Errorf("%v footer contains %q: %s", size, notice, m.footer())
				}
			}
			if !strings.Contains(m.header(), "intake · "+state) {
				t.Errorf("missing %s: %s", state, m.header())
			}
			key(m, '?')
			if !strings.Contains(m.View().Content, "sockets: last event 3h47m0s ago") {
				t.Error("freshness detail missing from help")
			}
		}
	}
}

func TestBlockActivitySpark(t *testing.T) {
	b := []view.ActivityBucket{{Events: 1, Sample: &model.MachineSample{Load1: 1, Cores: 4}}, {}, {Sample: &model.MachineSample{Cores: 4}}, {Events: 8, Sample: &model.MachineSample{Load1: 8, Cores: 4}}}
	for _, load := range []bool{false, true} {
		for _, width := range []int{1, 2, 4, 90} {
			got := activitySpark(b, width, load)
			if lipgloss.Width(got) != min(width, len(b)) {
				t.Errorf("width %d: %q", width, got)
			}
			for _, r := range got {
				if !strings.ContainsRune("▁▂▃▄▅▆▇█·", r) {
					t.Errorf("undocumented glyph %q", r)
				}
			}
			if width >= 4 && !strings.Contains(got, "·") {
				t.Error("null minute lost")
			}
		}
	}
	if got := activitySpark(b, 4, false); got != "▁·▁█" {
		t.Errorf("events: %q", got)
	}
	if got := activitySpark(b, 2, false); got != "·█" {
		t.Errorf("bins must preserve gaps and peaks: %q", got)
	}
}

func TestActivityCurrentAndMaxFit(t *testing.T) {
	s := fixture()
	s.snap.Activity.Machines = []view.ActivityMachine{{Buckets: []view.ActivityBucket{
		{Events: 99, Sample: &model.MachineSample{Load1: 12, Cores: 4}},
		{Events: 3, Sample: &model.MachineSample{Load1: 2, Cores: 4}},
	}}}
	for _, width := range []int{40, 80, 120, 160} {
		m := setup(t, s, Options{}).(*app)
		m.width, m.height = width, 100
		for _, line := range strings.Split(m.homeView(), "\n") {
			if strings.Contains(line, "events  ") || strings.Contains(line, "load    ") && strings.ContainsAny(line, "▁▂▃▄▅▆▇█") {
				if lipgloss.Width(line) != width {
					t.Errorf("width %d: %q", width, line)
				}
				if !strings.Contains(line, "max") {
					t.Errorf("max missing: %s", line)
				}
			}
		}
		text := m.homeView()
		if !strings.Contains(text, "3/min max 99") || !strings.Contains(text, "2.0 max 12.0") {
			t.Errorf("current/max missing: %s", text)
		}
	}
}

func TestLoadSparkAboveCoresIsCritical(t *testing.T) {
	b := []view.ActivityBucket{{Sample: &model.MachineSample{Load1: 2, Cores: 4}}, {Sample: &model.MachineSample{Load1: 8, Cores: 4}}}
	got := styledSpark(b, 2, true, false)
	if !strings.Contains(got, view.TokenStyle("crit", false).Render("█")) {
		t.Fatalf("above cores lacks crit color: %q", got)
	}
	if !strings.Contains(got, view.TokenStyle("ink", false).Render("▂")) {
		t.Fatalf("normal load color: %q", got)
	}
}

func TestActivityAxisMatchesBinnedRows(t *testing.T) {
	s := fixture()
	buckets := make([]view.ActivityBucket, 60)
	for i := range buckets {
		buckets[i] = view.ActivityBucket{Events: 10, Sample: &model.MachineSample{Load1: 2, Cores: 4}}
	}
	s.snap.Activity.Machines = []view.ActivityMachine{{Buckets: buckets}}
	m := setup(t, s, Options{}).(*app)
	m.width, m.height = 80, 100
	var chartEnd, axisEnd int
	for _, line := range strings.Split(m.homeView(), "\n") {
		if strings.Contains(line, "events  ") {
			for i, r := range []rune(line) {
				if strings.ContainsRune("▁▂▃▄▅▆▇█", r) {
					chartEnd = i + 1
				}
			}
		}
		if strings.Contains(line, "−60m") {
			axisEnd = lipgloss.Width(strings.Split(line, "now")[0]) + 3
		}
	}
	if chartEnd != axisEnd {
		t.Fatalf("time axis ends at %d, binned chart at %d", axisEnd, chartEnd)
	}
}
