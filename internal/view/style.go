package view

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// StateStyle gives state labels and charts the same meaning across readers.
func StateStyle(label string, noColor bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if noColor {
		return s
	}
	switch strings.TrimPrefix(label, "~") {
	case "working":
		return s.Foreground(lipgloss.Color("2"))
	case "waiting":
		return s.Foreground(lipgloss.Color("3"))
	case "stalled?", "dead":
		return s.Foreground(lipgloss.Color("1"))
	case "idle":
		return s.Foreground(lipgloss.Color("#5fafff"))
	case "finished":
		return s.Faint(true)
	}
	return s
}

// GaugePercent clamps inconsistent samples and handles machines without capacity.
func GaugePercent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return min(100, float64(used)/float64(total)*100)
}

// GaugeLevel uses the same thresholds in browser and terminal readers.
func GaugeLevel(used, total uint64) string {
	percent := GaugePercent(used, total)
	if percent >= 90 {
		return "critical"
	}
	if percent >= 75 {
		return "warning"
	}
	return "normal"
}

func GaugeStyle(used, total uint64, noColor bool) lipgloss.Style {
	state := "working"
	switch GaugeLevel(used, total) {
	case "critical":
		state = "stalled?"
	case "warning":
		state = "waiting"
	}
	return StateStyle(state, noColor)
}
