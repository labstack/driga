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
	token := strings.TrimSuffix(strings.TrimPrefix(label, "~"), "?")
	if token == "dead" {
		token = "stalled"
	}
	switch token {
	case "working", "waiting", "stalled", "idle", "finished":
		return TokenStyle(token, noColor)
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
	token := "ok"
	switch GaugeLevel(used, total) {
	case "critical":
		token = "crit"
	case "warning":
		token = "warn"
	}
	return TokenStyle(token, noColor)
}

// LoadLevel compares the measured load with this machine's capacity.
func LoadLevel(load float64, cores int) string {
	if cores <= 0 {
		return "normal"
	}
	if load > float64(cores) {
		return "critical"
	}
	if load >= float64(cores)*.75 {
		return "warning"
	}
	return "normal"
}
