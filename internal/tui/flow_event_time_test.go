package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
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
