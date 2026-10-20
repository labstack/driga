package tui

import (
	"github.com/labstack/driga/internal/view"
	"strings"
	"testing"
	"time"
)

func TestFlowMotionOff(t *testing.T) {
	for _, opts := range []Options{{NoColor: true}, {MotionOff: true}} {
		now := time.Now()
		opts.Now = func() time.Time { return now }
		m := setup(t, fixture(), opts).(*app)
		m.flow = flowFixture()
		m.width = 100
		m.recordFlowEvent(view.TailEvent{Key: "new", MachineID: "local", Adapter: "claude", ExternalID: "lead"})
		if strings.Contains(m.flowView(), "◉") {
			t.Fatal("motion enabled")
		}
		m.flowGrowthPulse = map[string]flowGrowthPulse{"one": {until: now.Add(time.Second)}}
		if m.flowAnimation() != nil {
			t.Fatal("scheduled motion with motion disabled")
		}
	}
}
