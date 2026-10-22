package derive

import (
	"sort"
	"time"

	"github.com/labstack/driga/internal/model"
)

type MCPTrend struct {
	ResolutionSeconds int       `json:"resolution_seconds"`
	Host              MCPHost   `json:"host"`
	Key               string    `json:"command_key"`
	Name              string    `json:"name"`
	State             string    `json:"state"`
	StartedCount      int       `json:"started_count"`
	EndedCount        int       `json:"ended_count"`
	NetGrowth         int       `json:"net_growth"`
	FirstSample       time.Time `json:"first_sample"`
	LastSample        time.Time `json:"last_sample"`
	RatePerHour       float64   `json:"rate_per_hour"`
	Count             int       `json:"count"`
	Memory            uint64    `json:"memory_bytes"`
	IdentityComplete  bool      `json:"identity_complete"`
}

type mcpTrendKey struct {
	pid     int32
	start   int64
	command string
}

func trendKey(host MCPHost, command string) mcpTrendKey {
	return mcpTrendKey{host.PID, host.StartedAt.UnixNano(), command}
}

type mcpMember struct {
	pid   int32
	start int64
}
type mcpTrendState struct {
	trend    MCPTrend
	initial  int
	previous map[mcpMember]bool
}
type MCPTrendBuilder struct {
	groups map[mcpTrendKey]*mcpTrendState
}

func NewMCPTrendBuilder() *MCPTrendBuilder {
	return &MCPTrendBuilder{groups: map[mcpTrendKey]*mcpTrendState{}}
}

// Add consumes chronological observations. The first retained sample is a
// baseline, never evidence that its already-running servers were added.
func (b *MCPTrendBuilder) Add(s model.MCPObservation) {
	k := trendKey(s.Host, s.Key)
	g := b.groups[k]
	first := g == nil
	if first {
		g = &mcpTrendState{initial: s.Count, trend: MCPTrend{Host: s.Host, Key: s.Key, Name: s.Name, FirstSample: s.At, IdentityComplete: !s.Host.StartedAt.IsZero()}}
		b.groups[k] = g
	} else if !s.At.After(g.trend.LastSample) {
		return
	}
	current := make(map[mcpMember]bool, len(s.Members))
	for _, p := range s.Members {
		current[mcpMember{p.PID, p.StartedAt.UnixNano()}] = true
		if p.PID <= 0 || p.StartedAt.IsZero() {
			g.trend.IdentityComplete = false
		}
	}
	if len(current) != s.Count {
		g.trend.IdentityComplete = false
	}
	if !first {
		for p := range current {
			if !g.previous[p] {
				g.trend.StartedCount++
			}
		}
		for p := range g.previous {
			if !current[p] {
				g.trend.EndedCount++
			}
		}
	}
	g.previous = current
	g.trend.ResolutionSeconds = max(g.trend.ResolutionSeconds, s.ResolutionSeconds)
	t := &g.trend
	t.LastSample, t.Count, t.Memory = s.At, s.Count, s.Footprint
	t.NetGrowth = s.Count - g.initial
	t.Host.Age = max(0, s.At.Sub(t.Host.StartedAt))
	if t.Host.StartedAt.IsZero() {
		t.Host.Age = 0
	}
	if hours := t.LastSample.Sub(t.FirstSample).Hours(); hours > 0 {
		t.RatePerHour = float64(t.StartedCount) / hours
	}
	t.State = "steady"
	if t.EndedCount > 0 {
		t.State = "churning"
	} else if t.IdentityComplete && t.StartedCount >= 2 {
		t.State = "leaking"
	}
}

func (b *MCPTrendBuilder) Trends() []MCPTrend {
	result := make([]MCPTrend, 0, len(b.groups))
	for _, g := range b.groups {
		result = append(result, g.trend)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Memory != b.Memory {
			return a.Memory > b.Memory
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.Host.PID != b.Host.PID {
			return a.Host.PID < b.Host.PID
		}
		return a.Host.StartedAt.Before(b.Host.StartedAt)
	})
	return result
}

// ApplyMCPTrends keeps each host's evidence separate, including in command
// rollups. Rates and observation windows from different hosts are never summed.
func ApplyMCPTrends(v *MCPServerView, trends []MCPTrend) {
	v.Trends = trends
	byCommand := map[string][]MCPTrend{}
	byHost := map[mcpTrendKey]MCPTrend{}
	current := map[mcpTrendKey]bool{}
	for _, s := range v.Samples {
		current[trendKey(s.Host, s.Key)] = true
	}
	for _, t := range trends {
		k := trendKey(t.Host, t.Key)
		if !current[k] {
			continue
		}
		byHost[k] = t
		byCommand[t.Key] = append(byCommand[t.Key], t)
	}
	for i := range v.Commands {
		v.Commands[i].Trends = byCommand[v.Commands[i].Key]
	}
	for i := range v.Pileups {
		p := &v.Pileups[i]
		if t, ok := byHost[trendKey(p.Host, p.Key)]; ok {
			p.Trends = []MCPTrend{t}
		}
	}
}
