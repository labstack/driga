package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
)

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
