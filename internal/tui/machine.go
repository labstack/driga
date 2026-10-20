package tui

import (
	"fmt"
	"path"
	"strconv"

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
