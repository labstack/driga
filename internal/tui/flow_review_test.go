package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestFlowRecentEventsUseLocalTime(t *testing.T) {
	original := time.Local
	time.Local = time.FixedZone("fixture local", -7*60*60)
	t.Cleanup(func() { time.Local = original })
	m := setup(t, fixture(), Options{}).(*app)
	m.flow = flowFixture()
	m.flow.Lead.Events = []model.Event{{TS: time.Date(2026, 10, 20, 15, 26, 46, 0, time.UTC), Kind: "local-clock-fixture"}}
	got := strings.Join(m.flowDetailLines(), "\n")
	if !strings.Contains(got, "08:26:46 local-clock-fixture") || strings.Contains(got, "15:26:46") {
		t.Fatalf("recent event must use local time: %s", got)
	}
}

func TestFlowAttentionCardsHaveSessionHints(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Tx(context.Background(), flowtest.SeedAmbiguousAttention); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{"titles", "none"} {
		f, err := view.Flow(st, view.Filter{CaptureText: policy}, 0, flowtest.Now)
		if err != nil {
			t.Fatal(err)
		}
		m := setup(t, fixture(), Options{Now: func() time.Time { return flowtest.Now }}).(*app)
		m.flow, m.width = f, 100
		got := m.flowView()
		want := []string{"sample · Fix cache", "sample · Fix parser", "sample · #ccc333", "sample · #ddd444"}
		if policy == "none" {
			want[0], want[1] = "sample · work/cache", "sample · work/parser"
		}
		for _, hint := range want {
			if !strings.Contains(got, hint) {
				t.Errorf("%s missing %q: %s", policy, hint, got)
			}
		}
	}
}

func TestFlowFooterAndHelpDescribeMapKeys(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab, m.width, m.height = flowTab, 140, 60
	m.flow = flowFixture()
	for _, text := range []string{m.footer()} {
		for _, want := range []string{"j/k session", "enter jump", "[/] family", "-/+ zoom"} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %q: %s", want, text)
			}
		}
	}
	for _, want := range []string{"esc back", "? help", "q quit"} {
		if !strings.Contains(m.footer(), want) {
			t.Error("footer missing", want)
		}
	}
	m.help = true
	for _, stale := range []string{"j k worker", "j/k worker", "enter open", "[ / ] session"} {
		if strings.Contains(m.footer(), stale) || strings.Contains(m.View().Content, "7 Flow · [ / ] session") {
			t.Error("stale Flow help:", stale)
		}
	}
}

func TestFlowTrackSemantics(t *testing.T) {
	m := setup(t, fixture(), Options{NoColor: true, MotionOff: true, Now: func() time.Time { return flowtest.Now }}).(*app)
	m.flow = flowFixture()
	m.width = 140
	text, hits := m.flowLayout()
	if strings.Contains(text, "╱") || strings.Contains(text, "▒") {
		t.Fatal("legacy hatch", text)
	}
	if strings.Count(text, "cpu ") != 1 {
		t.Fatal("CPU should appear only on selected row", text)
	}
	if strings.Count(text, "head = verdict as of") != 1 {
		t.Fatal("missing shared head explanation", text)
	}
	if !strings.Contains(text, "━━▶") || !strings.Contains(text, "░") {
		t.Fatal("head or no data missing", text)
	}
	if hits[0].height != 2 || hits[1].height != 1 {
		t.Fatal("row heights", hits)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "cpu ") && strings.Contains(line, "░") {
			t.Fatal("hatch spilled into CPU", line)
		}
	}
	for st, want := range map[string]string{"working": "━━▶", "waiting": "══◆", "idle": "··○", "stalled": "┄┄‖", "finished": "──●", "unknown": "┈┈?"} {
		m.flow.Lead.Mixer.Verdict = st
		if !strings.Contains(m.flowView(), want) {
			t.Error(st, want)
		}
	}
}

func TestFlowParentConnectorThroughBlankCells(t *testing.T) {
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return flowtest.Now }}).(*app)
	m.flow = flowFixture()
	m.width = 140
	// The parent connection crosses a sibling which already ended.
	end := flowtest.Now.Add(-49 * time.Minute)
	m.flow.Children[0].EndedAt = &end
	text, hits := m.flowLayout()
	lines := strings.Split(text, "\n")
	row := lines[hits[1].y]
	// Panel borders use │ too; the interior track must contain its own connector.
	if strings.Count(row, "│") < 3 {
		t.Fatal("parent connector lost in blank track cells", row)
	}
}

func TestFlowHintsAppearOnceInFooter(t *testing.T) {
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return flowtest.Now }}).(*app)
	m.flow = flowFixture()
	m.tab = flowTab
	m.loaded = true
	m.width = 140
	m.height = 80
	text := m.View().Content
	if strings.Count(text, "enter jump") != 1 {
		t.Fatal("flow repeats its keyboard hints", text)
	}
}
