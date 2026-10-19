package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/barchart"
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
		row := tabRow{attention: &a, machine: a.MachineID, key: key, agent: a.ID, cells: []string{"", a.State, a.Project, a.Summary}}
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

// panel keeps detail panes consistent with Home's rounded frames.
func panel(title, body string, width int) string { return framedPanel(title, "", body, width, true) }

func framedPanel(title, hint, body string, width int, noColor bool) string {
	width = max(4, width)
	inner := width - 4
	border := view.TokenStyle("line", noColor)
	heading := " " + truncate(clean(title), max(1, width-6)) + " "
	hint = clean(hint)
	room := width - 2 - lipgloss.Width(heading) - 2
	right := ""
	if room > 4 && hint != "" {
		right = " " + truncate(hint, room-2) + " "
	}
	headingStyle := view.TokenStyle("ink", noColor)
	if !noColor {
		headingStyle = headingStyle.Bold(true)
	}
	top := border.Render("╭") + headingStyle.Render(heading) + border.Render(strings.Repeat("─", max(0, width-2-lipgloss.Width(heading)-lipgloss.Width(right)))) + view.TokenStyle("faint", noColor).Render(right) + border.Render("╮")
	lines := []string{top}
	for _, line := range strings.Split(body, "\n") {
		line = truncate(line, inner)
		lines = append(lines, border.Render("│")+" "+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+" "+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return strings.Join(lines, "\n")
}

func (m *app) homeView() string {
	cols := homeColumns(m.width)
	needsW, statesW, machineW := m.width, m.width, m.width
	if cols == 3 {
		needsW = m.width / 2
		statesW = (m.width - needsW - 2) * 2 / 5
		machineW = m.width - needsW - statesW - 2
	}
	if cols == 2 {
		needsW = (m.width - 1) * 3 / 5
		statesW = m.width - needsW - 1
		machineW = statesW
	}
	dim := view.TokenStyle("faint", m.opts.NoColor)
	mid := view.TokenStyle("muted", m.opts.NoColor)
	needs := []string{}
	start := max(0, m.cursors[homeTab]-5)
	for i := start; i < min(len(m.rows), start+6); i++ {
		r := m.rows[i]
		a := r.attention
		state := r.cells[1]
		title := r.cells[3]
		location := r.cells[2]
		adapter := ""
		age := ""
		if a != nil {
			if a.Title != "" {
				title = a.Title
			}
			adapter = a.Adapter
			if d, ok := m.snap.AgentDetails[a.ID]; ok && d.Session.Branch != "" {
				location += " · " + d.Session.Branch
			}
			if !a.At.IsZero() {
				age = duration(m.snap.ObservedAt.Sub(a.At))
			}
		}
		inner := needsW - 4
		prefix := m.stateText("▌", state) + " " + m.stateText(fmt.Sprintf("%-8s", state), state) + " " + mid.Render(fmt.Sprintf("%-6s", truncate(clean(adapter), 6))) + " "
		inline := needsW >= 76
		locationWidth := 0
		if inline {
			locationWidth = min(24, max(0, (inner-lipgloss.Width(prefix)-lipgloss.Width(age))/2))
		}
		available := max(1, inner-lipgloss.Width(prefix)-lipgloss.Width(age)-locationWidth-2)
		text := truncate(clean(title), available)
		line := prefix + text
		if inline {
			line += strings.Repeat(" ", max(1, available-lipgloss.Width(text)+1)) + dim.Render(truncate(clean(location), locationWidth))
		}
		line += strings.Repeat(" ", max(1, inner-lipgloss.Width(line+age))) + m.stateText(age, state)
		if i == m.cursors[homeTab] && !m.opts.NoColor {
			line = lipgloss.NewStyle().Background(view.ThemeColor("sunk")).Render(line)
		}
		needs = append(needs, line)
		if !inline {
			needs = append(needs, dim.Render("  "+truncate(clean(location), inner-2)))
		}
	}
	if len(needs) == 0 {
		needs = append(needs, "Nothing needs you")
	}
	if len(m.rows) > 6 {
		needs = append(needs, dim.Render(fmt.Sprintf("%d more · 3 Inbox", len(m.rows)-6)))
	}
	counts := map[string]int{}
	total := 0
	m.eachAgent(func(a view.AgentRow) { counts[strings.TrimPrefix(a.Label, "~")]++; total++ })
	stateOrder := []string{"working", "waiting", "stalled?", "idle", "finished", "dead", "unknown"}
	segmented := ""
	legend := []string{}
	used := 0
	seen := 0
	for _, state := range stateOrder {
		n := counts[state]
		seen += n
		end := seen * (statesW - 4) / max(1, total)
		if n > 0 {
			segmented += m.stateText(strings.Repeat("█", end-used), state)
			used = end
		}
		if n > 0 || state != "dead" && state != "unknown" {
			legend = append(legend, m.stateText(fmt.Sprintf("● %d %s", n, state), state))
		}
	}
	if total == 0 {
		segmented = dim.Render(strings.Repeat("░", statesW-4))
	}
	states := segmented + "\n" + styledPack(legend, statesW-4, "  ")
	machine := []string{}
	hosts := m.snap.Machine.Machines
	if len(hosts) == 0 {
		hosts = []view.MachineView{m.snap.Machine}
	}
	for _, host := range hosts {
		if !m.machineVisible(host.MachineID) {
			continue
		}
		sample := host.Sample
		if len(hosts) > 1 {
			machine = append(machine, mid.Render(clean(m.machineName(host.MachineID))))
		}
		if sample.TS.IsZero() {
			machine = append(machine, "Health unavailable")
			continue
		}
		for _, metric := range []struct {
			name        string
			used, total uint64
		}{{"memory", sample.MemUsed, sample.MemTotal}, {"swap", sample.SwapUsed, sample.SwapTotal}} {
			if metric.total == 0 {
				machine = append(machine, mid.Render(metric.name+" unavailable"))
				continue
			}
			n := max(1, machineW-4-14)
			machine = append(machine, mid.Render(fmt.Sprintf("%-7s", metric.name))+gaugeBar(metric.used, metric.total, n, m.opts.NoColor)+" "+view.GaugeStyle(metric.used, metric.total, m.opts.NoColor).Render(fmt.Sprintf("%3.0f%%", view.GaugePercent(metric.used, metric.total))), dim.Render(fmt.Sprintf("        %s/%s", humanBytes(metric.used), humanBytes(metric.total))))
		}
		token := "ok"
		if view.LoadLevel(sample.Load1, sample.Cores) == "critical" {
			token = "crit"
		} else if view.LoadLevel(sample.Load1, sample.Cores) == "warning" {
			token = "warn"
		}
		machine = append(machine, mid.Render("load    ")+view.TokenStyle(token, m.opts.NoColor).Render(fmt.Sprintf("%.1f", sample.Load1))+dim.Render(fmt.Sprintf(" / %d cores", sample.Cores)), mid.Render("disk    ")+humanBytes(sample.DiskFree)+dim.Render(" free"))
	}
	if len(machine) == 0 {
		machine = append(machine, "Health unavailable")
	}
	activity := []string{}
	sparkW := max(1, m.width-4-8-10)
	for _, a := range m.snap.Activity.Machines {
		if !m.machineVisible(a.MachineID) {
			continue
		}
		if len(m.snap.Activity.Machines) > 1 {
			activity = append(activity, mid.Render(clean(m.machineName(a.MachineID))))
		}
		eventsNow, loadNow := "—", "—"
		if len(a.Buckets) > 0 {
			last := a.Buckets[len(a.Buckets)-1]
			if last.Sample != nil || last.Events > 0 {
				eventsNow = fmt.Sprintf("%d/min", last.Events)
			}
			if last.Sample != nil {
				loadNow = fmt.Sprintf("%.1f", last.Sample.Load1)
			}
		}
		activity = append(activity, dim.Render("events  ")+styledSpark(a.Buckets, sparkW, false, m.opts.NoColor)+"  "+view.TokenStyle("idle", m.opts.NoColor).Render(eventsNow), dim.Render("load    ")+styledSpark(a.Buckets, sparkW, true, m.opts.NoColor)+"  "+view.TokenStyle("ink", m.opts.NoColor).Render(loadNow))
		cores := 0
		first := -1
		for i, b := range a.Buckets {
			if b.Sample != nil {
				cores = b.Sample.Cores
			}
			if first < 0 && (b.Sample != nil || b.Events > 0) {
				first = i
			}
		}
		if cores > 0 {
			activity = append(activity, view.TokenStyle("crit", m.opts.NoColor).Render(fmt.Sprintf("        ┄ %d cores reference", cores)))
		}
		if first > 0 {
			activity = append(activity, dim.Render(fmt.Sprintf("        no history before collector start · %dm collected", len(a.Buckets)-first)))
		}
		if !view.ActivityHasSamples(a.Buckets) {
			activity = append(activity, dim.Render(view.HistoryEmpty))
		}
		if !view.ActivityHasEvents(a.Buckets) {
			activity = append(activity, dim.Render(view.EventsEmpty))
		}
	}
	if len(activity) == 0 {
		activity = append(activity, dim.Render(view.HistoryEmpty), dim.Render(view.EventsEmpty))
	}
	axis := []string{"−60m", "−45m", "−30m", "−15m", "now"}
	axisLine := ""
	for i, label := range axis {
		pos := i * (sparkW - 3) / 4
		axisLine += strings.Repeat(" ", max(0, pos-lipgloss.Width(axisLine))) + label
	}
	activity = append(activity, dim.Render("        "+axisLine))
	projectW := m.width
	leftW := m.width
	if cols == 3 {
		projectW = needsW + statesW + 1
		leftW = machineW
	}
	projects := []string{dim.Render("PROJECT         AGENTS                         BRANCH          DIRTY  ↑   LATEST")}
	for _, p := range view.SortedProjects(m.snap.Projects.Rows) {
		if !m.machineVisible(p.MachineID) {
			continue
		}
		agents := []view.AgentRow{}
		branch, title := "", ""
		m.eachAgent(func(a view.AgentRow) {
			if a.MachineID == p.MachineID && a.Project == p.Status.Repo.Name {
				agents = append(agents, a)
				if branch == "" {
					branch = a.Branch
				}
				if title == "" {
					title = a.Title
				}
			}
		})
		detail := m.snap.ProjectDetails[p.Status.Repo.ID]
		if branch == "" && len(detail.WorktreeRows) > 0 {
			branch = detail.WorktreeRows[0].Branch
		}
		if len(detail.Handoffs) > 0 {
			title = detail.Handoffs[0].Text
		} else if len(p.Status.Where) > 0 {
			title = p.Status.Where[0].Text
		}
		mini := ""
		parts := []string{}
		segments := 0
		for _, state := range view.StateCounts(agents) {
			n := min(state.Count, max(0, 6-segments))
			mini += m.stateText(strings.Repeat("▮", n), state.Label)
			segments += n
			parts = append(parts, fmt.Sprintf("%d %s", state.Count, state.Label))
		}
		mini += dim.Render(strings.Repeat("▯", max(0, 6-segments)))
		summary := strings.Join(parts, " · ")
		if summary == "" {
			summary = "quiet"
		}
		name := fmt.Sprintf("%-14s", truncate(clean(p.Status.Repo.Name), 14))
		line := name + " " + mini + " " + fmt.Sprintf("%-22s", truncate(summary, 22)) + " " + fmt.Sprintf("%-15s", truncate(clean(branch), 15)) + fmt.Sprintf(" %5d %3d  ", p.Dirty, p.Unpushed) + clean(title)
		if projectW < 90 {
			line = name + " " + mini + " " + summary + "\n  " + clean(branch) + fmt.Sprintf(" · dirty %d · ↑%d · ", p.Dirty, p.Unpushed) + clean(title)
		}
		if p.Status.LiveAgents == 0 && p.Status.Activity == "idle" && !m.opts.NoColor {
			line = lipgloss.NewStyle().Faint(true).Render(line)
		}
		projects = append(projects, line)
	}
	if len(projects) == 1 {
		projects = append(projects, "No projects")
	}
	summary := m.visibleSummary()
	leftovers := []string{fmt.Sprintf("orphans    %d · ~%s reclaimable", summary.Orphans, humanBytes(summary.Reclaim))}
	ready, refused := 0, 0
	var history int64
	for _, c := range m.snap.Cleanup {
		if !m.machineVisible(m.snap.LocalMachineID) {
			continue
		}
		if c.Kind == cleanup.History {
			history += c.Size
		}
		if c.Kind == cleanup.Worktree {
			if c.Status == "ready" {
				ready++
			} else {
				refused++
			}
		}
	}
	if m.snap.Cleanup == nil {
		leftovers = append(leftovers, "worktrees  not loaded · 6 Cleanup")
	} else {
		leftovers = append(leftovers, fmt.Sprintf("worktrees  %d ready · %d kept", ready, refused))
	}
	if history > 0 {
		leftovers = append(leftovers, fmt.Sprintf("history    %.1f MB measured", float64(history)/(1<<20)))
	} else {
		leftovers = append(leftovers, dim.Render("history    not measured · r in Cleanup"))
	}
	sourceNames := []string{}
	for name := range m.snap.Freshness {
		if strings.Contains(name, "hook") || strings.Contains(name, "spool") || strings.Contains(name, "otlp") {
			sourceNames = append(sourceNames, name)
		}
	}
	sort.Strings(sourceNames)
	for _, name := range sourceNames {
		f := m.snap.Freshness[name]
		status := "source healthy"
		token := "working"
		if f.Status == "not-yet-receiving" {
			status = "waiting for first event"
			token = "waiting"
		} else if f.Stale || f.LastError != "" {
			status = "stale"
			token = "waiting"
		} else if f.LastOKAt.IsZero() {
			status = "no events received"
			token = "finished"
		}
		label := "hooks"
		if strings.Contains(name, "otlp") {
			label = "telemetry"
		}
		leftovers = append(leftovers, mid.Render(label+"  ")+view.TokenStyle(token, m.opts.NoColor).Render("● "+clean(name)+" · "+status))
	}
	frame := func(title, hint, body string, width int) string {
		return framedPanel(title, hint, body, width, m.opts.NoColor)
	}
	needsPanel := frame(fmt.Sprintf("Needs you %d", len(m.rows)), "enter open · x ack", strings.Join(needs, "\n"), needsW)
	statesPanel := frame(fmt.Sprintf("Agents %d", total), "by state", states, statesW)
	machinePanel := frame("Machine", "", strings.Join(machine, "\n"), machineW)
	rows := []string{}
	switch cols {
	case 3:
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, needsPanel, " ", statesPanel, " ", machinePanel))
	case 2:
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, needsPanel, " ", statesPanel+"\n"+machinePanel))
	default:
		rows = append(rows, needsPanel, statesPanel, machinePanel)
	}
	rows = append(rows, frame("Activity · 60 min", "events/min · load · ┄ cores", strings.Join(activity, "\n"), m.width))
	pp := frame("Projects", "2 Projects for all", strings.Join(projects, "\n"), projectW)
	lp := frame("Leftovers", "6 Cleanup", strings.Join(leftovers, "\n"), leftW)
	if cols == 3 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, pp, " ", lp))
	} else {
		rows = append(rows, pp, lp)
	}
	return strings.Join(rows, "\n")
}

func (m *app) stateText(text string, label ...string) string {
	state := text
	if len(label) > 0 {
		state = label[0]
	} else if fields := strings.Fields(text); len(fields) > 0 {
		state = fields[0]
	}
	return view.StateStyle(state, m.opts.NoColor).Render(text)
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

// activitySpark uses two braille columns per cell. Missing collection remains dots.
func activitySpark(buckets []view.ActivityBucket, width int, load bool) string {
	return styledSpark(buckets, width, load, true)
}

func styledSpark(buckets []view.ActivityBucket, width int, load, noColor bool) string {
	if len(buckets) == 0 {
		return view.HistoryEmpty
	}
	width = max(1, width)
	count := width * 2
	values := make([]float64, count)
	valid := make([]bool, count)
	ceiling := 1.0
	first := -1
	for i, b := range buckets {
		if b.Sample != nil || b.Events > 0 {
			first = i
			break
		}
	}
	for x := range count {
		from := x * len(buckets) / count
		to := min(len(buckets), max(from+1, (x+1)*len(buckets)/count))
		valid[x] = true
		for i := from; i < to; i++ {
			b := buckets[i]
			v := float64(b.Events)
			if first < 0 || i < first {
				valid[x] = false
				continue
			}
			if load {
				if b.Sample == nil {
					valid[x] = false
					continue
				}
				v = b.Sample.Load1
				ceiling = max(ceiling, float64(b.Sample.Cores))
			}
			values[x] = max(values[x], v)
		}
		ceiling = max(ceiling, values[x])
	}
	bits := [][]rune{{0x40, 0x04, 0x02, 0x01}, {0x80, 0x20, 0x10, 0x08}}
	token := "idle"
	if load {
		token = "ink"
	}
	var out strings.Builder
	for x := 0; x < count; x += 2 {
		if !valid[x] || !valid[x+1] {
			out.WriteString(view.TokenStyle("faint", noColor).Render("·"))
			continue
		}
		r := rune(0x2800)
		for column := 0; column < 2; column++ {
			height := min(4, int(values[x+column]/ceiling*4+.5))
			for y := 0; y < height; y++ {
				r |= bits[column][y]
			}
		}
		out.WriteString(view.TokenStyle(token, noColor).Render(string(r)))
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
