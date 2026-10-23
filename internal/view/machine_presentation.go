package view

import (
	"fmt"

	"github.com/labstack/driga/internal/derive"
)

func MCPProcessLabel(c derive.MCPCommand) string {
	label := "helper processes"
	if derive.MCPNameToken(c.Name) {
		label = "servers"
	}
	return fmt.Sprintf("%d %s", c.Count, label)
}
func AttributionText(a *MachineAttribution) string {
	if a == nil {
		return "unattributed"
	}
	proof := "started by"
	if a.Proof == "cwd_started_after" {
		proof = "in project, started later"
	}
	return fmt.Sprintf("%s %s · session #%d", proof, a.Project, a.SessionID)
}
func MachineSummary(v MachineView) string {
	a, s := v.AgentShare, v.Sample
	total := a.MachineMemory
	if total == 0 {
		total = s.MemUsed
	}
	pct := float64(0)
	if total > 0 {
		pct = 100 * float64(a.Memory) / float64(total)
	}
	text := fmt.Sprintf("agents %s of %s (%.0f%%)", HumanBytes(a.Memory), HumanBytes(total), pct)
	if a.LikelyMemory > 0 {
		text += " · likely +" + HumanBytes(a.LikelyMemory)
	}
	return text + fmt.Sprintf(" · %.1f%% CPU · %d processes · %d MCP servers · pressure %s · swap %s/%s · disk %s free", a.CPUPercent, a.Processes, a.MCPServers, s.PressureLevel, HumanBytes(s.SwapUsed), HumanBytes(s.SwapTotal), HumanBytes(s.DiskFree))
}

// MachineOtherApps falls back to the available ranking totals for older peers.
func MachineOtherApps(v MachineView) MachineApp {
	if v.OtherApps.ID != "" {
		return v.OtherApps
	}
	a := MachineApp{ID: "other", Name: "Other apps", Processes: []MachineProcess{}}
	for _, app := range v.TopMemory {
		a.Memory += app.Memory
		a.CPUPercent += app.CPUPercent
		a.MemoryMeasure = mergeMeasure(a.MemoryMeasure, app.MemoryMeasure)
	}
	return a
}

// FormatLoad preserves native numeric formatting and labels unavailable loads.
func FormatLoad(load float64, format string) string {
	if load < 0 {
		return "unknown"
	}
	return fmt.Sprintf(format, load)
}
