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
	for _, r := range m.snap.Machines.Rows {
		add(tabRow{machine: r.MachineID, cells: []string{"", m.machineName(r.MachineID), "", r.State, m.machineAge(r.MachineID)}})
	}
	hosts := m.snap.Machine.Machines
	if len(hosts) == 0 {
		hosts = []view.MachineView{m.snap.Machine}
	}
	for _, host := range hosts {
		s := host.Sample
		for _, metric := range [][3]string{
			{"memory", humanBytes(s.MemUsed) + "/" + humanBytes(s.MemTotal), ""},
			{"compressed", humanBytes(s.MemCompressed), ""},
			{"swap", humanBytes(s.SwapUsed) + "/" + humanBytes(s.SwapTotal), ""},
			{"pressure level", s.PressureLevel, ""},
			{"pressure free", fmt.Sprintf("%.1f%%", s.PressureFree), ""},
			{"load (1/5/15)", fmt.Sprintf("%.1f / %.1f / %.1f", s.Load1, s.Load5, s.Load15), ""},
			{"cores", strconv.Itoa(s.Cores), ""}, {"disk free", humanBytes(s.DiskFree), ""},
		} {
			add(tabRow{machine: host.MachineID, cells: []string{"", metric[0], "", metric[1], metric[2]}})
		}
		add(tabRow{machine: host.MachineID, cells: []string{"", "MCP servers", "", "enter details · read-only", ""}})
		for _, c := range host.MCPServers.Commands {
			add(tabRow{machine: host.MachineID, mcpCommand: &c, cells: []string{"", c.Name, fmt.Sprintf("%d servers", c.Count), fmt.Sprintf("%d hosts · %d duplicates", c.HostCount, c.DuplicateCount), humanBytes(c.Memory)}})
		}
		if len(host.MCPServers.Pileups) == 0 {
			add(tabRow{machine: host.MachineID, cells: []string{"", "No MCP pile-ups", "", "", ""}})
		}
		for _, p := range host.MCPServers.Pileups {
			add(tabRow{machine: host.MachineID, pileup: &p, cells: []string{"", p.Name + " pile-up", fmt.Sprint(p.Count), fmt.Sprintf("%s PID %d · %s", p.Host.Name, p.Host.PID, duration(p.Host.Age)), humanBytes(p.Memory)}})
		}
		for _, rank := range []struct {
			name, key string
			apps      []view.MachineApp
		}{{"Top 10 apps by memory", "mem", host.TopMemory}, {"Top 10 apps by CPU", "cpu", host.TopCPU}} {
			add(tabRow{machine: host.MachineID, cells: []string{"", rank.name, "", "→ expand · enter details", ""}})
			if len(rank.apps) == 0 {
				add(tabRow{machine: host.MachineID, cells: []string{"", "No process readings", "", "", ""}})
			}
			for _, a := range rank.apps {
				key := "machine:" + host.MachineID + ":" + rank.key + ":" + a.ID
				mark := "▸ "
				if m.expanded[key] {
					mark = "▾ "
				}
				add(tabRow{machine: host.MachineID, key: key, app: &a, cells: []string{"", mark + a.Name, fmt.Sprintf("%.1f%%", a.CPUPercent), a.MemoryMeasure, humanBytes(a.Memory)}})
				if m.expanded[key] {
					for _, p := range a.Processes {
						detail := p.MemoryMeasure
						if p.Attribution != nil {
							detail = fmt.Sprintf("← %s · session #%d", p.Attribution.Project, p.Attribution.SessionID)
						}
						add(tabRow{machine: host.MachineID, cwd: p.Cwd, key: key + fmt.Sprint(p.PID), app: &a, cells: []string{"", fmt.Sprintf("  %d %s", p.PID, path.Base(p.Exe)), fmt.Sprintf("%.1f%%", p.CPUPercent), detail, humanBytes(p.Memory)}})
					}
				}
			}
		}
		if len(host.Memory) > 0 {
			add(tabRow{machine: host.MachineID, cells: []string{"", "Agent memory by role", "", "", ""}})
			for _, r := range host.Memory {
				add(tabRow{machine: r.MachineID, cells: []string{"", r.Agent, "", r.Role, humanBytes(r.RSS)}})
			}
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
		if row < 0 || row >= len(m.rows) || m.rows[row].mcpCommand == nil {
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
		text += view.MCPCommandSummary(*r.mcpCommand)
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
