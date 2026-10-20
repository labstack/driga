package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/view"
)

type flowObservationFrameMsg struct{}
type flowGrowthPulse struct {
	until time.Time
	frame int
	bytes int64
}

func growthKey(n view.FlowNode) string {
	return fmt.Sprintf("%s/%d/%s:%s", n.MachineID, n.ID, n.Adapter, n.ExternalID)
}

func (m *app) recordFlowObservations(f view.FlowView) {
	if f.Lead == nil {
		return
	}
	if m.flowGrowthSeen == nil {
		m.flowGrowthSeen = map[string]time.Time{}
	}
	if m.flowGrowthPulse == nil {
		m.flowGrowthPulse = map[string]flowGrowthPulse{}
	}
	for _, n := range append([]view.FlowNode{*f.Lead}, f.Children...) {
		key := growthKey(n)
		prev, seen := m.flowGrowthSeen[key]
		at := time.Time{}
		if n.Observed != nil {
			at = n.Observed.LastGrowthAt
		}
		if !seen || at.After(prev) {
			m.flowGrowthSeen[key] = at
		}
		if seen && at.After(prev) && n.Observed.LastGrowthBytes > 0 {
			m.flowGrowthPulse[key] = flowGrowthPulse{until: m.opts.Now().Add(time.Second), bytes: n.Observed.LastGrowthBytes}
		}
	}
}

// The regular one-second clock expires event-bound accents. No frame timer.
func (m *app) flowAnimation() tea.Cmd { return nil }

func (m *app) growthWire(n view.FlowNode, wire string) string {
	pulse, ok := m.flowGrowthPulse[growthKey(n)]
	if !ok || !m.opts.Now().Before(pulse.until) {
		return wire
	}
	chars := []rune(wire)
	// Skip the rail/junction prefix and the terminal arrow.
	start := 0
	if len(chars) > 6 {
		start = 3
	}
	pos := min(len(chars)-2, start+pulse.frame)
	chars[pos] = '●'
	return view.TokenStyle("observation", m.opts.NoColor).Render(string(chars))
}

func (m *app) observationLines(n view.FlowNode, width int) []string {
	label := "transcript · 60m"
	if pulse, ok := m.flowGrowthPulse[growthKey(n)]; ok && m.opts.Now().Before(pulse.until) {
		label = "+" + transcriptSize(pulse.bytes) + " transcript"
	}
	if n.Observed == nil {
		return []string{"transcript: not observed"}
	}
	return []string{label, view.TokenStyle("observation", m.opts.NoColor).Render(view.ObservationSpark(n.Observed.LogBytesPerMin, width))}
}

func transcriptSize(bytes int64) string {
	n := float64(max(0, bytes))
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	text := fmt.Sprintf("%.1f", n)
	text = strings.TrimSuffix(text, ".0")
	return text + " " + units[i]
}
