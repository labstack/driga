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
		return s.Foreground(lipgloss.Color("4"))
	case "finished":
		return s.Faint(true)
	}
	return s
}
