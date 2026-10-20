package view

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
)

func TestMCPTrendText(t *testing.T) {
	original := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = original })
	at := time.Date(2026, 10, 20, 8, 12, 0, 0, time.UTC)
	leak := derive.MCPTrend{Host: derive.MCPHost{PID: 100, Name: "host"}, State: "leaking", StartedCount: 2, NetGrowth: 2, FirstSample: at, LastSample: at.Add(160 * time.Minute), RatePerHour: .75}
	c := derive.MCPCommand{Name: "server", Trends: []derive.MCPTrend{{State: "steady"}, {State: "churning"}, leak}}
	if text := MCPCommandSummary(c); !strings.Contains(text, "leaking ↑ 0.75/h since 08:12") {
		t.Fatal(text)
	}
	for _, want := range []string{"host PID 100", "started 2", "ended 0", "net +2", "2026-10-20T08:12:00Z", "2026-10-20T10:52:00Z"} {
		if text := MCPCommandDetails(c); !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	c.Trends = c.Trends[:2]
	if text := MCPCommandSummary(c); !strings.Contains(text, "churning") {
		t.Fatal(text)
	}
	c.Trends = c.Trends[:1]
	if text := MCPCommandSummary(c); !strings.Contains(text, "steady") {
		t.Fatal(text)
	}
}
