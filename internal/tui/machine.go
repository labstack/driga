package tui

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/labstack/driga/internal/view"
)

func (m *app) machineRows(add func(tabRow)) {
	hosts := m.snap.Machine.Machines
	if len(hosts) == 0 {
		hosts = []view.MachineView{m.snap.Machine}
	}
	for _, host := range hosts {
		textRow := func(text string) {
			for _, line := range strings.Split(ansi.Wrap(text, max(1, m.width-4), ""), "\n") {
				add(tabRow{machine: host.MachineID, machineText: line, cells: []string{"", line, "", "", ""}})
			}
		}
		textRow(view.MachineSummary(host))
		textRow("Memory measure: " + host.AgentShare.MemoryMeasure + " · " + view.FootprintNote)
		textRow("Sessions by memory")
		for _, a := range view.MachineSessionApps(host) {
			m.machineAppRows(add, host.MachineID, "session", a)
			if _, surface, ok := strings.Cut(a.Name, " · started from "); ok {
				textRow("Started from " + surface)
			}
		}

		textRow("Agent processes without a session")
		for _, a := range host.AgentNoSession {
			m.machineAppRows(add, host.MachineID, "agent", a)
		}
		textRow("MCP servers and helper processes")
		for _, c := range host.MCPServers.Commands {
			add(tabRow{machine: host.MachineID, mcpCommand: &c, cells: []string{"", c.Name, view.MCPProcessLabel(c), fmt.Sprintf("%d hosts · %d duplicates", c.HostCount, c.DuplicateCount), view.HumanBytes(c.Memory)}})
		}
		if len(host.MCPServers.Pileups) == 0 {
			textRow("No MCP pile-ups")
		}
		for _, p := range host.MCPServers.Pileups {
			add(tabRow{machine: host.MachineID, pileup: &p, cells: []string{"", p.Name + " pile-up", fmt.Sprint(p.Count), fmt.Sprintf("%s PID %d · %s", p.Host.Name, p.Host.PID, duration(p.Host.Age)), view.HumanBytes(p.Memory)}})
		}
		if len(host.LikelyAgent) > 0 {
			textRow("Likely agent work (shared with editor)")
			for _, a := range host.LikelyAgent {
				m.machineAppRows(add, host.MachineID, "likely", a)
			}
		}
		other := view.MachineOtherApps(host)
		key := "machine:" + host.MachineID + ":other"
		mark := "▸ "
		if m.expanded[key] {
			mark = "▾ "
		}
		add(tabRow{machine: host.MachineID, key: key, machineText: mark + "Other apps · " + view.HumanBytes(other.Memory) + " (" + other.MemoryMeasure + ")", cells: []string{"", mark + "Other apps", "", other.MemoryMeasure, view.HumanBytes(other.Memory)}})
		if m.expanded[key] {
			for _, rank := range []struct {
				name, key string
				apps      []view.MachineApp
			}{{"Top 10 apps by memory", "mem", host.TopMemory}, {"Top 10 apps by CPU", "cpu", host.TopCPU}} {
				textRow(rank.name)
				for _, a := range rank.apps {
					m.machineAppRows(add, host.MachineID, rank.key, a)
				}
			}
		}
		textRow("Machine vitals")
		s := host.Sample
		for _, metric := range [][2]string{
			{"memory", view.HumanBytes(s.MemUsed) + "/" + view.HumanBytes(s.MemTotal)},
			{"compressed", view.HumanBytes(s.MemCompressed)},
			{"swap", view.HumanBytes(s.SwapUsed) + "/" + view.HumanBytes(s.SwapTotal)},
			{"pressure level", s.PressureLevel}, {"pressure free", fmt.Sprintf("%.1f%%", s.PressureFree)},
			{"load (1/5/15)", view.FormatLoad(s.Load1, "%.1f") + " / " + view.FormatLoad(s.Load5, "%.1f") + " / " + view.FormatLoad(s.Load15, "%.1f")},
			{"cores", strconv.Itoa(s.Cores)}, {"disk free", view.HumanBytes(s.DiskFree)},
		} {
			add(tabRow{machine: host.MachineID, cells: []string{"", metric[0], "", metric[1], ""}})
		}
		textRow("Agent memory by role")
		for _, r := range host.Memory {
			add(tabRow{machine: r.MachineID, cells: []string{"", r.Agent, "", r.Role, view.HumanBytes(r.RSS)}})
		}
	}
}
func (m *app) machineAppRows(add func(tabRow), machine, rank string, a view.MachineApp) {
	key := "machine:" + machine + ":" + rank + ":" + a.ID
	mark := "▸ "
	if m.expanded[key] {
		mark = "▾ "
	}
	add(tabRow{machine: machine, key: key, app: &a, cells: []string{"", mark + a.Name, fmt.Sprintf("%.1f%%", a.CPUPercent), a.MemoryMeasure, view.HumanBytes(a.Memory)}})
	if m.expanded[key] {
		for _, p := range a.Processes {
			detail := p.MemoryMeasure
			if p.Attribution != nil {
				detail = view.AttributionText(p.Attribution)
			}
			add(tabRow{machine: machine, cwd: p.Cwd, key: key + fmt.Sprint(p.PID), app: &a, cells: []string{"", fmt.Sprintf("  %d %s", p.PID, path.Base(p.Exe)), fmt.Sprintf("%.1f%%", p.CPUPercent), detail, view.HumanBytes(p.Memory)}})
		}
	}
}

// MCP summaries span the machine table's columns so their labels and counts
// remain readable at terminal widths. Keep one line per row for navigation.
func (m *app) machineTable(body string) string {
	lines := strings.Split(body, "\n")
	cursorLine := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(ansi.Strip(line)), "›") {
			cursorLine = i
			break
		}
	}
	if cursorLine < 0 {
		return body
	}
	for i := 1; i < len(lines); i++ {
		row := m.cursors[m.tab] + i - cursorLine
		if row < 0 || row >= len(m.rows) || (m.rows[row].mcpCommand == nil && m.rows[row].machineText == "") {
			continue
		}
		r := m.rows[row]
		text := "  "
		if row == m.cursors[m.tab] {
			text = "› "
		}
		if len(r.cells) == 6 {
			text += m.machineName(r.machine) + " · "
		}
		if r.mcpCommand != nil {
			text += view.MCPCommandSummary(*r.mcpCommand)
		} else {
			text += r.machineText
		}
		if r.stale {
			text += " (stale " + m.machineAge(r.machine) + ")"
		}
		text = lipgloss.NewStyle().Width(m.width).Render(truncate(clean(text), m.width))
		if !m.opts.NoColor {
			style := lipgloss.NewStyle()
			if r.stale {
				style = style.Faint(true)
			}
			if row == m.cursors[m.tab] {
				style = style.Reverse(true)
			}
			text = style.Render(text)
		}
		lines[i] = text
	}
	return strings.Join(lines, "\n")
}
