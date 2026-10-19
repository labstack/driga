package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

type tabRow struct {
	attention *view.Attention
	cleanup   *cleanup.Candidate
	machine   string
	stale     bool
	key       string
	cells     []string
	agent     int64
	project   int64
	cwd       string
	inbox     *model.InboxItem
	orphan    *model.Orphan
}

func (m *app) tabVisible(tab int) bool {
	if m.opts.Project == "" {
		return true
	}
	switch tab {
	case homeTab:
		return true
	case 0:
		return len(m.snap.Agents.Rows) > 0
	case 1:
		return len(m.snap.Projects.Rows) > 0
	case 2:
		return len(m.snap.Inbox.Rows) > 0
	case 3:
		return len(m.snap.Orphans.Rows) > 0
	case 4:
		return !m.snap.Machine.Sample.TS.IsZero() || len(m.snap.Machine.Memory) > 0
	case 5:
		return len(m.snap.Cleanup) > 0 || m.opts.CleanupList != nil
	}
	return false
}
func (m *app) tabBar() string {
	s := m.visibleSummary()
	projects, inbox, orphans := 0, 0, 0
	for _, r := range m.snap.Projects.Rows {
		if m.machineVisible(r.MachineID) {
			projects++
		}
	}
	for _, r := range m.snap.Inbox.Rows {
		if m.machineVisible(r.MachineID) {
			inbox++
		}
	}
	for _, r := range m.snap.Orphans.Rows {
		if m.machineVisible(r.MachineID) {
			orphans++
		}
	}
	cleanupCount := 0
	if m.machineVisible(m.localMachine()) {
		cleanupCount = len(m.snap.Cleanup)
	}
	names := []string{fmt.Sprintf("Agents %d", s.Agents), fmt.Sprintf("Projects %d", projects), "Inbox", fmt.Sprintf("Orphans %d", orphans), "Machine", fmt.Sprintf("Cleanup %d", cleanupCount)}
	tabs := []string{}
	active := func(label string, selected bool) string {
		style := view.TokenStyle("muted", m.opts.NoColor)
		if selected && m.opts.NoColor {
			return "[" + label + "]"
		}
		if selected {
			style = view.TokenStyle("ink", m.opts.NoColor)
			if !m.opts.NoColor {
				style = style.Bold(true).Underline(true)
			}
		}
		return style.Render(label)
	}
	tabs = append(tabs, active("0 Home", m.tab == homeTab))
	for i, name := range names {
		if !m.tabVisible(i) {
			continue
		}
		label := active(fmt.Sprintf("%d %s", i+1, name), m.tab == i)
		if i == 2 && m.opts.NoColor {
			label = active(fmt.Sprintf("%d %s %d", i+1, name, inbox), m.tab == i)
		}
		if i == 2 && !m.opts.NoColor {
			token := "faint"
			if inbox > 0 {
				token = "waiting"
			}
			label += " " + view.TokenStyle(token, m.opts.NoColor).Render(fmt.Sprint(inbox))
		}
		tabs = append(tabs, label)
	}
	return styledPack(tabs, m.width, "   ")
}

// styledPack keeps ANSI styling while wrapping only at item boundaries.
func styledPack(parts []string, width int, separator string) string {
	lines := []string{}
	line := ""
	for _, part := range parts {
		part = truncate(part, width)
		if line != "" && lipgloss.Width(line+separator+part) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += separator
		}
		line += part
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m *app) rebuild() {
	if !m.tabVisible(m.tab) {
		for i := 0; i < 6; i++ {
			if m.tabVisible(i) {
				m.tab = i
				break
			}
		}
	}
	var titles []string
	var weights []int
	rows := []tabRow{}
	query := strings.ToLower(m.filter.Value())
	add := func(r tabRow) {
		if !m.machineVisible(r.machine) {
			return
		}
		r.stale = m.machineStale(r.machine)
		if r.stale {
			r.cells[len(r.cells)-1] += " (stale " + m.machineAge(r.machine) + ")"
		}
		if query != "" && !strings.Contains(strings.ToLower(strings.Join(r.cells, " ")), query) {
			return
		}
		rows = append(rows, r)
	}
	switch m.tab {
	case homeTab:
		titles = []string{"", "STATE", "PROJECT", "SUMMARY"}
		weights = []int{2, 10, 14, 40}
		for _, row := range m.needsYou() {
			add(row)
		}
	case 5:
		titles = []string{"", "KIND", "STATUS", "PATH"}
		weights = []int{2, 10, 18, 50}
		for _, c := range m.snap.Cleanup {
			add(tabRow{machine: m.snap.LocalMachineID, key: c.ID, cleanup: &c, cwd: c.Path, cells: []string{"", string(c.Kind), c.Status, c.Path}})
		}
	case 0:
		titles = []string{"", "STATE", "AGENT", "PROJECT", "BRANCH", "TITLE / LAST PROMPT", "AGE", "MEM"}
		weights = []int{2, 10, 8, 10, 10, 24, 6, 6}
		var agents func([]view.AgentRow, int)
		agents = func(rs []view.AgentRow, depth int) {
			rs = append([]view.AgentRow(nil), rs...)
			sort.SliceStable(rs, func(i, j int) bool {
				a, b := rs[i], rs[j]
				if m.group && a.Project != b.Project {
					return a.Project < b.Project
				}
				switch m.sort {
				case 0:
					return a.Label < b.Label
				case 1:
					return a.Age > b.Age
				case 2:
					return a.Mem > b.Mem
				default:
					return a.Project < b.Project
				}
			})
			for _, r := range rs {
				key := "agent:" + strconv.FormatInt(r.ID, 10)
				mark := ""
				if len(r.Children) > 0 {
					mark = "▸ "
					if m.expanded[key] {
						mark = "▾ "
					}
				}
				title := r.Title
				if r.FinishedChildren > 0 {
					title += fmt.Sprintf(" (%d workers finished)", r.FinishedChildren)
				}
				add(tabRow{machine: r.MachineID, key: key, agent: r.ID, cells: []string{"", r.Label, strings.Repeat(" ", depth) + mark + r.Adapter, r.Project, r.Branch, title, agentAge(r.Age), humanBytes(r.Mem)}})
				// Filtering searches through collapsed families as well.
				if m.expanded[key] || query != "" {
					agents(r.Children, depth+1)
				}
			}
		}
		agents(m.snap.Agents.Rows, 0)
	case 1:
		titles = []string{"", "PROJECT", "ACTIVITY", "AGENTS", "DIRTY", "UNPUSHED", "WORKTREES", "WHERE"}
		weights = []int{2, 14, 9, 7, 6, 9, 10, 25}
		for _, r := range m.snap.Projects.Rows {
			key := "project:" + strconv.FormatInt(r.Status.Repo.ID, 10)
			where := ""
			if len(r.Status.Where) > 0 {
				p := r.Status.Where[0]
				where = fmt.Sprintf("%s (%s, %s)", p.Text, p.Source, duration(m.snap.ObservedAt.Sub(p.At)))
				if p.Source == "claude" {
					where = fmt.Sprintf("%s · claude %s", p.Text, duration(m.snap.ObservedAt.Sub(p.At)))
				}
				if p.Stale {
					where += " (stale)"
				}
			}
			name := r.Status.Repo.Name
			if r.Worktrees > 0 {
				mark := "▸ "
				if m.expanded[key] {
					mark = "▾ "
				}
				name = mark + name
			}
			add(tabRow{machine: r.MachineID, key: key, project: r.Status.Repo.ID, cwd: r.Status.Repo.Path, cells: []string{"", name, r.Status.Activity, strconv.Itoa(r.Status.LiveAgents), strconv.Itoa(r.Dirty), strconv.Itoa(r.Unpushed), strconv.Itoa(r.Worktrees), where}})
			if m.expanded[key] {
				for _, wt := range m.snap.ProjectDetails[r.Status.Repo.ID].WorktreeRows {
					add(tabRow{machine: r.MachineID, key: key, project: r.Status.Repo.ID, cwd: wt.Path, cells: []string{"", " ↳ " + wt.Branch, "worktree", "", strconv.Itoa(wt.Dirty + wt.Untracked), strconv.Itoa(wt.Ahead), "", wt.Path}})
				}
			}
		}
	case 2:
		titles = []string{"", "ID", "KIND", "AGE", "PROJECT", "SUMMARY"}
		weights = []int{2, 16, 9, 6, 12, 35}
		for _, r := range m.snap.Inbox.Rows {
			item := r.InboxItem
			add(tabRow{machine: r.MachineID, key: "inbox:" + r.MachineID + ":" + r.ID, agent: r.SessionID, inbox: &item, cells: []string{"", r.ID, r.Kind, duration(r.Age), r.Project, r.Summary}})
		}
	case 3:
		titles = []string{"", "ID", "AGE", "SIGNATURE", "PROCS", "~RECLAIM", "CWD", "VERIFIED", "EVIDENCE"}
		weights = []int{2, 14, 6, 12, 6, 10, 14, 9, 20}
		for _, r := range m.snap.Orphans.Rows {
			o := r.Orphan
			o.MachineID = r.MachineID
			age := duration(r.Age)
			if o.Young {
				age = "young"
			}
			add(tabRow{machine: r.MachineID, key: "orphan:" + r.MachineID + ":" + o.ID, cwd: o.Cwd, orphan: &o, cells: []string{"", o.ID, age, o.Signature, strconv.Itoa(o.ProcCount), "~" + humanBytes(o.RSSTotal), o.Cwd, strconv.FormatBool(o.Verified), strings.Join(o.Evidence, "; ")}})
		}
	case 4:
		titles = []string{"", "METRIC / AGENT", "VALUE / ROLE", "MEM"}
		weights = []int{2, 30, 28, 20}
		for _, r := range m.snap.Machines.Rows {
			add(tabRow{machine: r.MachineID, cells: []string{"", m.machineName(r.MachineID), r.State, m.machineAge(r.MachineID)}})
		}
		hosts := m.snap.Machine.Machines
		if len(hosts) == 0 {
			hosts = []view.MachineView{m.snap.Machine}
		}
		for _, host := range hosts {
			s := host.Sample
			for _, r := range [][3]string{{"memory", humanBytes(s.MemUsed) + "/" + humanBytes(s.MemTotal), ""}, {"compressed", humanBytes(s.MemCompressed), ""}, {"swap", humanBytes(s.SwapUsed) + "/" + humanBytes(s.SwapTotal), ""}, {"load (1/5/15)", fmt.Sprintf("%.1f / %.1f / %.1f", s.Load1, s.Load5, s.Load15), ""}, {"cores", strconv.Itoa(s.Cores), ""}, {"disk free", humanBytes(s.DiskFree), ""}, {"pressure free", fmt.Sprintf("%.1f", s.PressureFree), ""}} {
				add(tabRow{machine: host.MachineID, cells: []string{"", r[0], r[1], r[2]}})
			}
		}
		for _, r := range m.snap.Machine.Memory {
			add(tabRow{machine: r.MachineID, cells: []string{"", r.Agent, r.Role, humanBytes(r.RSS)}})
		}
	}
	ids := map[string]bool{}
	for _, r := range rows {
		if r.machine != "" {
			ids[r.machine] = true
		}
	}
	if len(ids) > 1 && m.tab != homeTab {
		titles = append([]string{titles[0], "MACHINE"}, titles[1:]...)
		weights = append([]int{weights[0], 14}, weights[1:]...)
		for i := range rows {
			rows[i].cells = append([]string{rows[i].cells[0], m.machineName(rows[i].machine)}, rows[i].cells[1:]...)
		}
	}
	m.rows = rows
	m.cursors[m.tab] = min(max(0, m.cursors[m.tab]), max(0, len(rows)-1))
	columns := make([]table.Column, len(titles))
	sum := 0
	for _, w := range weights {
		sum += w
	}
	widths := make([]int, len(titles))
	tableWidth := m.width
	if m.splitDetail() {
		tableWidth = m.width / 2
	}
	available := max(1, tableWidth-len(titles))
	for i := range widths {
		widths[i] = max(1, weights[i]*available/sum)
	}
	used := 0
	for _, w := range widths {
		used += w
	}
	if used < available {
		widths[len(widths)-1] += available - used
	}
	cells := make([]table.Row, len(rows))
	for i, r := range rows {
		cells[i] = make(table.Row, len(titles))
		for j, c := range r.cells {
			cells[i][j] = truncate(clean(c), widths[j])
			if titles[j] == "STATE" || titles[j] == "KIND" {
				cells[i][j] = view.StateStyle(c, m.opts.NoColor).Render(cells[i][j])
			}
			if r.stale && !m.opts.NoColor {
				cells[i][j] = lipgloss.NewStyle().Faint(true).Render(cells[i][j])
			}
		}
		if i == m.cursors[m.tab] {
			cells[i][0] = "›"
		}
	}
	for i := range columns {
		columns[i] = table.Column{Title: titles[i], Width: widths[i]}
	}
	style := lipgloss.NewStyle().PaddingRight(1)
	styles := table.Styles{Header: style, Cell: style, Selected: lipgloss.NewStyle()}
	if !m.opts.NoColor {
		styles.Header = style.Bold(true)
		styles.Selected = lipgloss.NewStyle().Reverse(true)
	}
	m.table.SetRows(nil)
	m.table.SetStyles(styles)
	m.table.SetColumns(columns)
	m.table.SetRows(cells)
	m.table.SetWidth(tableWidth)
	m.table.SetHeight(m.bodyHeight())
	m.table.SetCursor(m.cursors[m.tab])
	m.filter.SetWidth(max(1, m.width-2))
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.bodyHeight())
	if m.detail {
		m.viewport.SetContent(m.detailText)
	}
}

func (m *app) selected() (tabRow, bool) {
	if m.detail && m.detailRow != nil {
		return *m.detailRow, true
	}
	i := m.cursors[m.tab]
	if i < 0 || i >= len(m.rows) {
		return tabRow{}, false
	}
	return m.rows[i], true
}
func (m *app) selectedCwd() string {
	r, ok := m.selected()
	if !ok {
		return ""
	}
	if r.cwd != "" {
		return r.cwd
	}
	return m.snap.AgentDetails[r.agent].Session.Cwd
}

// agentAge renders an unknown (zero) session age as "—".
func agentAge(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	return duration(d)
}
