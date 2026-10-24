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
			if lipgloss.Width(got) != width {
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

func TestLoadSparkAboveCoresIsCritical(t *testing.T) {
	b := []view.ActivityBucket{{Sample: &model.MachineSample{Load1: 2, Cores: 4}}, {Sample: &model.MachineSample{Load1: 8, Cores: 4}}}
	for _, width := range []int{2, 5} {
		got := styledSpark(b, width, true, false)
		if !strings.Contains(got, view.TokenStyle("crit", false).Render(strings.Repeat("█", width/2))) {
			t.Fatalf("above cores lacks crit color: %q", got)
		}
		if !strings.Contains(got, view.TokenStyle("ink", false).Render(strings.Repeat("▂", (width+1)/2))) {
			t.Fatalf("normal load color: %q", got)
		}
	}
}

func TestActivityExpandedMinutesAndGaps(t *testing.T) {
	buckets := []view.ActivityBucket{
		{Events: 1, Sample: &model.MachineSample{Load1: 1}},
		{},
		{Sample: &model.MachineSample{}},
		{Events: 8, Sample: &model.MachineSample{Load1: 8}},
	}
	for _, tc := range []struct {
		width int
		spans [4]int
	}{{4, [4]int{1, 1, 1, 1}}, {5, [4]int{2, 1, 1, 1}}, {10, [4]int{3, 3, 2, 2}}, {90, [4]int{23, 23, 22, 22}}} {
		for _, load := range []bool{false, true} {
			want := strings.Repeat("▁", tc.spans[0]) + strings.Repeat("·", tc.spans[1]) + strings.Repeat("▁", tc.spans[2]) + strings.Repeat("█", tc.spans[3])
			if got := activitySpark(buckets, tc.width, load); got != want {
				t.Errorf("width %d load %v: got %q, want %q", tc.width, load, got, want)
			}
			if got := styledSpark(buckets, tc.width, load, false); !strings.Contains(got, view.TokenStyle("faint", false).Render(strings.Repeat("·", tc.spans[1]))) {
				t.Errorf("width %d load %v: gap span lost faint style: %q", tc.width, load, got)
			}
		}
	}
}
