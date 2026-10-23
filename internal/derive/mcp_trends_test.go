package derive

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestMCPRecordedTrends(t *testing.T) {
	at := time.Date(2026, 10, 20, 8, 12, 0, 0, time.UTC)
	for _, tc := range []struct {
		name           string
		series         [][]int32
		state          string
		started, ended int
	}{
		{"steady", [][]int32{{1, 2}, {1, 2}, {1, 2}}, "steady", 0, 0},
		{"churn-flat-count", [][]int32{{1, 2}, {1, 3}, {1, 4}}, "churning", 2, 2},
		{"churn-shrinks", [][]int32{{1}, {1, 2, 3}, {1, 2}}, "churning", 2, 1},
		{"one-addition", [][]int32{{1}, {1, 2}, {1, 2}}, "steady", 1, 0},
		{"leaking", [][]int32{{1}, {1, 2}, {1, 2, 3}}, "leaking", 2, 0},
		{"all-ended", [][]int32{{1}, {1, 2}, {}}, "churning", 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewMCPTrendBuilder()
			for i, ids := range tc.series {
				s := model.MCPObservation{At: at.Add(time.Duration(i) * 80 * time.Minute), Key: "command:server", Name: "server", Host: MCPHost{PID: 100, StartedAt: at.Add(-time.Hour)}, Count: len(ids)}
				for _, pid := range ids {
					s.Members = append(s.Members, model.ProcessIdentity{PID: pid, StartedAt: at.Add(time.Duration(pid) * time.Second)})
				}
				b.Add(s)
			}
			got := b.Trends()[0]
			if got.State != tc.state || got.StartedCount != tc.started || got.EndedCount != tc.ended || got.NetGrowth != tc.started-tc.ended || !got.FirstSample.Equal(at) || !got.LastSample.Equal(at.Add(160*time.Minute)) || got.RatePerHour != float64(tc.started)/(160.0/60) {
				t.Fatalf("trend=%+v", got)
			}
		})
	}
}

func TestMCPTrendIdentityReuseAndUnknownStarts(t *testing.T) {
	at := time.Date(2026, 10, 20, 8, 12, 0, 0, time.UTC)
	b := NewMCPTrendBuilder()
	s := model.MCPObservation{At: at, Key: "server", Host: MCPHost{PID: 100, StartedAt: at.Add(-time.Hour)}, Count: 1, Members: []model.ProcessIdentity{{PID: 1, StartedAt: at}}}
	b.Add(s)
	s.At = at.Add(time.Minute)
	s.Members = []model.ProcessIdentity{{PID: 1, StartedAt: s.At}}
	b.Add(s)
	if got := b.Trends()[0]; got.State != "churning" || got.StartedCount != 1 || got.EndedCount != 1 {
		t.Fatal(got)
	}
	// Reused host PID begins a separate history, never inheriting a leak.
	s.Host.StartedAt = s.At
	b.Add(s)
	if got := b.Trends(); len(got) != 2 {
		t.Fatal(got)
	}
	// Missing identity evidence cannot establish a leak.
	b = NewMCPTrendBuilder()
	s.Host.StartedAt = time.Time{}
	for i := range 3 {
		s.At = at.Add(time.Duration(i) * time.Minute)
		s.Members = append(s.Members, model.ProcessIdentity{PID: int32(i + 2), StartedAt: s.At})
		s.Count = len(s.Members)
		b.Add(s)
	}
	if got := b.Trends()[0]; got.State == "leaking" {
		t.Fatal(got)
	}
}

func TestMCPTrendNewHostCannotInheritOldLeak(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	ps := []model.Proc{{PID: 100, Cmdline: "codex app-server", Argv: []string{"codex", "app-server"}, StartTime: at}, {PID: 1, PPID: 100, Cmdline: "node /opt/server.js", Argv: []string{"node", "/opt/server.js"}, StartTime: at}}
	v := MCPServers(ps, nil, at.Add(time.Hour))
	s := v.Samples[0]
	leak := MCPTrend{Host: s.Host, Key: s.Key, State: "leaking", Count: 1}
	ApplyMCPTrends(&v, []MCPTrend{leak})
	if len(v.Commands[0].Trends) != 1 {
		t.Fatal(v)
	}
	ps[0].StartTime = at.Add(time.Second)
	ps[1].StartTime = at.Add(2 * time.Second)
	v = MCPServers(ps, nil, at.Add(time.Hour))
	ApplyMCPTrends(&v, []MCPTrend{leak})
	if len(v.Commands[0].Trends) != 0 || len(v.Trends) != 1 {
		t.Fatal(v)
	}
}

func TestMCPSampleMembersExcludeLaunchers(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	ps := []model.Proc{
		{PID: 100, Cmdline: "claude", Argv: []string{"claude"}, StartTime: at},
		{PID: 1, PPID: 100, Cmdline: "node exec firecrawl-mcp", Argv: []string{"node", "exec", "firecrawl-mcp"}, StartTime: at, RSS: 10},
		{PID: 2, PPID: 1, Cmdline: "node /cache/node_modules/.bin/firecrawl-mcp", Argv: []string{"node", "/cache/node_modules/.bin/firecrawl-mcp"}, StartTime: at, RSS: 20},
	}
	v := MCPServers(ps, nil, at.Add(time.Hour))
	if len(v.Samples) != 1 || v.Samples[0].Count != 1 || v.Samples[0].Footprint != 30 || len(v.Samples[0].Members) != 1 || v.Samples[0].Members[0].PID != 2 || v.Samples[0].Key != v.Commands[0].Key {
		t.Fatal(v)
	}
}
