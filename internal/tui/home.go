package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/barchart"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/view"
)

const homeTab = 6

func (m *app) needsYou() []tabRow {
	inbox := []view.InboxRow{}
	for _, r := range m.snap.Inbox.Rows {
		if m.machineVisible(r.MachineID) {
			inbox = append(inbox, r)
		}
	}
	agents := []view.AgentRow{}
	m.eachAgent(func(r view.AgentRow) { r.Children = nil; agents = append(agents, r) })
	rows := []tabRow{}
	for _, a := range view.NeedsYou(inbox, agents, m.snap.AgentDetails) {
		key := fmt.Sprintf("attention:%s:%d", a.MachineID, a.ID)
		row := tabRow{machine: a.MachineID, key: key, agent: a.ID, cells: []string{"", a.State, a.Project, a.Summary}}
		if a.Inbox != nil {
			item := a.Inbox.InboxItem
			item.MachineID = a.MachineID
			row.inbox = &item
			row.key = "inbox:" + a.MachineID + ":" + item.ID
		}
		rows = append(rows, row)
	}
	return rows
}

func (m *app) eachAgent(fn func(view.AgentRow)) {
	var walk func([]view.AgentRow)
	walk = func(rows []view.AgentRow) {
		for _, r := range rows {
			if m.machineVisible(r.MachineID) {
				fn(r)
			}
			walk(r.Children)
		}
	}
	walk(m.snap.Agents.Rows)
}

func homeColumns(width int) int {
	if width >= 150 {
		return 3
	}
	if width >= 110 {
		return 2
	}
	return 1
}

func panel(title, body string, width int) string {
	inner := max(1, width-4)
	lines := strings.Split(title+"\n"+body, "\n")
	for i := range lines {
		lines[i] = truncate(lines[i], inner)
	}
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}

func (m *app) homeView() string {
	cols := homeColumns(m.width)
	w := (m.width - (cols - 1)) / cols
	inner := max(1, w-4)
	needs := []string{}
	// Keep the selected attention row visible even when the queue is long.
	start := max(0, m.cursors[homeTab]-5)
	for i := start; i < min(len(m.rows), start+6); i++ {
		r := m.rows[i]
		mark := "  "
		if i == m.cursors[homeTab] {
			mark = "› "
		}
		state := clean(r.cells[1])
		origin := ""
		if r.machine != "" && len(m.snap.Machines.Rows) > 1 {
			origin = clean(m.machineName(r.machine)) + " · "
		}
		needs = append(needs, mark+origin+m.stateText(state)+" · "+clean(r.cells[2])+" · "+clean(r.cells[3]))
	}
	if len(needs) == 0 {
		needs = append(needs, "Nothing needs you")
	}
	counts := map[string]int{}
	m.eachAgent(func(r view.AgentRow) { counts[strings.TrimPrefix(r.Label, "~")]++ })
	total := 0
	for _, n := range counts {
		total += n
	}
	states := []string{}
	for _, state := range []string{"working", "waiting", "stalled?", "idle", "finished", "dead", "unknown"} {
		if counts[state] == 0 && (state == "dead" || state == "unknown") {
			continue
		}
		label := fmt.Sprintf("%-8s %3d ", state, counts[state])
		states = append(states, m.stateText(label)+bar(float64(counts[state]), float64(max(1, total)), max(1, inner-13), view.StateStyle(state, m.opts.NoColor)))
	}
	machine := []string{}
	hosts := m.snap.Machine.Machines
	if len(hosts) == 0 {
		hosts = []view.MachineView{m.snap.Machine}
	}
	for _, host := range hosts {
		if !m.machineVisible(host.MachineID) {
			continue
		}
		s := host.Sample
		name := clean(m.machineName(host.MachineID))
		if m.machineStale(host.MachineID) {
			name += " (stale)"
		}
		machine = append(machine, name)
		if s.TS.IsZero() {
			machine = append(machine, "Health unavailable")
			continue
		}
		for _, metric := range []struct {
			name        string
			used, total uint64
		}{{"Mem", s.MemUsed, s.MemTotal}, {"Swap", s.SwapUsed, s.SwapTotal}} {
			if metric.total == 0 {
				machine = append(machine, metric.name+" unavailable")
				continue
			}
			machine = append(machine, fmt.Sprintf("%s %.0f%% · %s/%s", metric.name, view.GaugePercent(metric.used, metric.total), humanBytes(metric.used), humanBytes(metric.total)), gaugeBar(metric.used, metric.total, inner, m.opts.NoColor))
		}
		machine = append(machine, fmt.Sprintf("Load %.1f / %d cores · disk free %s", s.Load1, s.Cores, humanBytes(s.DiskFree)))
		for _, a := range m.snap.Activity.Machines {
			if a.MachineID == host.MachineID {
				machine = append(machine, "load", activitySpark(a.Buckets, inner, true))
			}
		}
	}
	if len(machine) == 0 {
		machine = append(machine, "Health unavailable")
	}
	activity := []string{"60m · peaks when condensed · · = gap"}
	for _, a := range m.snap.Activity.Machines {
		if !m.machineVisible(a.MachineID) {
			continue
		}
		name := clean(m.machineName(a.MachineID))
		if a.Stale || m.machineStale(a.MachineID) {
			name += " (stale)"
		}
		activity = append(activity, name, "events/min", activitySpark(a.Buckets, inner, false))
		if !view.ActivityHasEvents(a.Buckets) {
			activity = append(activity, strings.Split(wrapText(view.EventsEmpty, inner), "\n")...)
		}
		activity = append(activity, "load", activitySpark(a.Buckets, inner, true))
		if !view.ActivityHasSamples(a.Buckets) {
			activity = append(activity, strings.Split(wrapText(view.HistoryEmpty, inner), "\n")...)
		}
	}
	if len(activity) == 1 {
		activity = append(activity, strings.Split(wrapText(view.HistoryEmpty, inner), "\n")...)
		activity = append(activity, strings.Split(wrapText(view.EventsEmpty, inner), "\n")...)
	}
	projects := []string{}
	for _, p := range view.SortedProjects(m.snap.Projects.Rows) {
		if !m.machineVisible(p.MachineID) {
			continue
		}
		dots, branch, title := "", "", ""
		agents := []view.AgentRow{}
		m.eachAgent(func(a view.AgentRow) {
			if a.MachineID == p.MachineID && a.Project == p.Status.Repo.Name {
				agents = append(agents, a)
				dots += view.StateStyle(a.Label, m.opts.NoColor).Render("●")
				if branch == "" {
					branch = a.Branch
				}
				if title == "" {
					title = a.Title
				}
			}
		})
		if len(agents) > 5 {
			parts := []string{}
			for _, state := range view.StateCounts(agents) {
				parts = append(parts, view.StateStyle(state.Label, m.opts.NoColor).Render(fmt.Sprintf("%d %s", state.Count, state.Label)))
			}
			dots = strings.Join(parts, " · ")
		}
		detail := m.snap.ProjectDetails[p.Status.Repo.ID]
		if branch == "" && len(detail.WorktreeRows) > 0 {
			branch = detail.WorktreeRows[0].Branch
		}
		if len(detail.Handoffs) > 0 {
			title = detail.Handoffs[0].Text
		} else if len(p.Status.Where) > 0 {
			title = p.Status.Where[0].Text
		}
		line := fmt.Sprintf("%s %s · %s · dirty %d · ↑%d · %s", clean(p.Status.Repo.Name), dots, clean(branch), p.Dirty, p.Unpushed, clean(title))
		if p.Status.LiveAgents == 0 && p.Status.Activity == "idle" && !m.opts.NoColor {
			line = lipgloss.NewStyle().Faint(true).Render(line)
		}
		projects = append(projects, line)
	}
	if len(projects) == 0 {
		projects = append(projects, "No projects")
	}
	summary := m.visibleSummary()
	leftovers := []string{fmt.Sprintf("%d orphans · ~%.1f MB reclaimable", summary.Orphans, float64(summary.Reclaim)/(1<<20))}
	ready, refused := 0, 0
	var history int64
	for _, c := range m.snap.Cleanup {
		if !m.machineVisible(m.snap.LocalMachineID) {
			continue
		}
		if c.Kind == cleanup.History {
			history += c.Size
			continue
		}
		if c.Kind != cleanup.Worktree {
			continue
		}
		if c.Status == "ready" {
			ready++
		} else {
			refused++
		}
	}
	if m.snap.Cleanup == nil {
		leftovers = append(leftovers, "Worktrees: not loaded (6 Cleanup)")
	} else {
		leftovers = append(leftovers, fmt.Sprintf("Worktrees: %d ready · %d refused/review", ready, refused))
	}
	if history > 0 {
		leftovers = append(leftovers, fmt.Sprintf("Agent history: %.1f MB measured", float64(history)/(1<<20)))
	}
	panels := []string{panel(fmt.Sprintf("Needs you (%d)", len(m.rows)), strings.Join(needs, "\n"), w), panel("Agent states", strings.Join(states, "\n"), w), panel("Machine", strings.Join(machine, "\n"), w), panel("Activity", strings.Join(activity, "\n"), w), panel("Projects", strings.Join(projects, "\n"), w), panel("Leftovers", strings.Join(leftovers, "\n"), w)}
	rows := []string{}
	for i := 0; i < len(panels); i += cols {
		parts := []string{}
		for j := i; j < min(i+cols, len(panels)); j++ {
			if j > i {
				parts = append(parts, " ")
			}
			parts = append(parts, panels[j])
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return strings.Join(rows, "\n")
}

func (m *app) stateText(label string) string {
	return view.StateStyle(strings.Fields(label)[0], m.opts.NoColor).Render(label)
}

func bar(value, total float64, width int, style lipgloss.Style) string {
	b := barchart.New(max(1, width), 1, barchart.WithHorizontalBars(), barchart.WithNoAxis(), barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(max(1, total)), barchart.WithBarGap(0))
	b.Push(barchart.BarData{Values: []barchart.BarValue{{Value: min(value, total), Style: style}}})
	b.Draw()
	return b.View()
}

// gaugeBar draws the full capacity, including the unused track at zero usage.
func gaugeBar(used, total uint64, width int, noColor bool) string {
	width = max(1, width)
	filled := min(width, int(view.GaugePercent(used, total)*float64(width)/100))
	track := strings.Repeat("░", width-filled)
	if !noColor {
		track = lipgloss.NewStyle().Faint(true).Render(track)
	}
	return view.GaugeStyle(used, total, noColor).Render(strings.Repeat("█", filled)) + track
}

func activitySpark(buckets []view.ActivityBucket, width int, load bool) string {
	if len(buckets) == 0 {
		return view.HistoryEmpty
	}
	width = max(1, width)
	values := make([]float64, width)
	valid := make([]bool, width)
	ceiling := 1.0
	for x := range width {
		valid[x] = true
		for i := x * len(buckets) / width; i < max(x*len(buckets)/width+1, (x+1)*len(buckets)/width); i++ {
			b := buckets[i]
			v := float64(b.Events)
			if load {
				if b.Sample == nil {
					valid[x] = false
					continue
				}
				v = b.Sample.Load1
			}
			values[x] = max(values[x], v)
		}
		ceiling = max(ceiling, values[x])
	}
	// Render contiguous runs separately so ntcharts never receives gap-as-zero data.
	var out strings.Builder
	for x := 0; x < width; {
		if !valid[x] {
			out.WriteString("·")
			x++
			continue
		}
		end := x + 1
		for end < width && valid[end] {
			end++
		}
		s := sparkline.New(end-x, 1)
		s.SetMax(ceiling)
		s.PushAll(values[x:end])
		s.Draw()
		out.WriteString(s.View())
		x = end
	}
	return out.String()
}

func (m *app) splitDetail() bool { return m.tab == 0 && m.width >= 140 && !m.detail }

func (m *app) agentsSplitView() string {
	left := m.width / 2
	right := m.width - left - 3
	text := "Select an agent"
	if r, ok := m.selected(); ok {
		if d, found := m.snap.AgentDetails[r.agent]; found {
			text = m.styledAgentDetail(d, max(1, right-4))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(left).Render(m.table.View()), " │ ", panel("Agent detail", text, right))
}

func (m *app) styledAgentDetail(d view.AgentView, width int) string {
	return m.styleAgentState(agentDetail(d, width), d.Label)
}

func (m *app) styleAgentState(text, label string) string {
	return strings.Replace(text, "State: "+clean(label), "State: "+view.StateStyle(label, m.opts.NoColor).Render(clean(label)), 1)
}
