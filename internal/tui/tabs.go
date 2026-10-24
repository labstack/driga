package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

type tabRow struct {
	prompt      *chat.Prompt
	machineText string
	mcpCommand  *derive.MCPCommand
	pileup      *derive.MCPPileup
	app         *view.MachineApp
	attention   *view.Attention
	cleanup     *cleanup.Candidate
	machine     string
	stale       bool
	key         string
	cells       []string
	agent       int64
	project     int64
	cwd         string
	inbox       *model.InboxItem
	orphan      *model.Orphan
}

func (m *app) tabVisible(tab int) bool {
	if m.opts.Project == "" {
		return true
	}
	switch tab {
	case flowTab, chatTab:
		return true
	case 0:
		return len(m.snap.Agents.Rows) > 0
	case 1:
		return len(m.snap.Projects.Rows) > 0
	case 2:
		return len(m.snap.Inbox.Rows) > 0 || len(m.prompts) > 0
	case 3:
		return len(m.snap.Orphans.Rows) > 0
	case 4:
		return !m.snap.Machine.Sample.TS.IsZero() || len(m.snap.Machine.Memory) > 0 || len(m.snap.Machine.MCPServers.Commands) > 0
	case 5:
		return len(m.snap.Cleanup) > 0 || m.opts.CleanupList != nil
	}
	return false
}
func (m *app) tabBar() string {
	text, _ := m.tabLayout()
	return text
}

func (m *app) tabLayout() (string, []tabHit) {
	selected := m.tab
	if selected == chatTab {
		selected = m.chatOrigin
	}
	if selected == 0 || selected == 2 {
		selected = flowTab
	}
	if selected == 3 || selected == 5 {
		selected = 4
	}
	labels := []string{"0 Flow", "1 Projects", "2 Machines"}
	indices := []int{flowTab, 1, 4}
	tabs := []string{}
	for i, label := range labels {
		if selected == indices[i] {
			if m.opts.NoColor {
				label = "[" + label + "]"
			} else {
				label = view.TokenStyle("ink", false).Bold(true).Underline(true).Render(label)
			}
		} else {
			label = view.TokenStyle("muted", m.opts.NoColor).Render(label)
		}
		tabs = append(tabs, label)
	}
	hits := []tabHit{}
	x, y := 0, 0
	for i, label := range tabs {
		w := lipgloss.Width(truncate(label, m.width))
		if x > 0 && x+3+w > m.width {
			x = 0
			y++
		}
		if x > 0 {
			x += 3
		}
		hits = append(hits, tabHit{x: x, y: y, width: w, tab: indices[i]})
		x += w
	}
	return styledPack(tabs, m.width, "   "), hits
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
	if m.tab == chatTab {
		m.rows = nil
		m.chatInput.SetWidth(max(1, m.width-4))
		return
	}
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
	case flowTab:
		m.syncBoard()
		titles = []string{"", "STATE", "SESSION"}
		weights = []int{2, 10, 40}
		for _, r := range m.boardRows() {
			add(tabRow{machine: r.Node.MachineID, key: r.Key, agent: r.Node.ID, cwd: r.Cwd, cells: []string{"", r.State, r.Title}})
		}

	case 5:
		titles = []string{"", "KIND", "STATUS", "PATH"}
		weights = []int{2, 10, 18, 50}
		for _, c := range m.snap.Cleanup {
			add(tabRow{machine: m.snap.LocalMachineID, key: c.ID, cleanup: &c, cwd: c.Path, cells: []string{"", string(c.Kind), view.FriendlyEnum(c.Status), c.Path}})
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
				title := r.Title + view.FromSuffix(r.StartedFrom)
				if r.FinishedChildren > 0 {
					title += fmt.Sprintf(" (%d workers finished)", r.FinishedChildren)
				}
				add(tabRow{machine: r.MachineID, key: key, agent: r.ID, cells: []string{"", r.Label, strings.Repeat(" ", depth) + mark + r.Adapter, r.Project, r.Branch, title, agentAge(r.Age), view.HumanBytes(r.Mem)}})
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
		titles = []string{"", "REASON", "TOOL", "WAIT / AGE", "PROJECT · BRANCH", "TITLE / DETAIL"}
		weights = []int{2, 18, 8, 10, 18, 32}
		for _, r := range m.snap.Inbox.Rows {
			if attention.Active(r.InboxItem) && !r.Historical {
				continue
			}
			item := r.InboxItem
			reason := r.Reason
			if reason == "" {
				reason = r.Kind
			}
			if r.Kind == "waiting" {
				reason = "[" + reason + "]"
			}
			if r.Historical {
				reason = "historical wait"
			}
			age := r.Age
			if r.Kind == "waiting" {
				age = attention.Waited(item, m.opts.Now())
			}
			title := r.Title
			if title == "" {
				title = r.Summary
			}
			if r.Detail != "" {
				title += " · " + r.Detail
			}
			add(tabRow{machine: r.MachineID, key: "inbox:" + r.MachineID + ":" + r.ID, agent: r.SessionID, inbox: &item, cells: []string{"", reason, r.Tool, duration(age), view.FlowJoin(r.Project, r.Branch), title}})
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
			add(tabRow{machine: r.MachineID, key: "orphan:" + r.MachineID + ":" + o.ID, cwd: o.Cwd, orphan: &o, cells: []string{"", o.ID, age, o.Signature, strconv.Itoa(o.ProcCount), "~" + view.HumanBytes(o.RSSTotal), o.Cwd, strconv.FormatBool(o.Verified), strings.Join(o.Evidence, "; ")}})
		}
	case 4:
		titles = []string{"", "METRIC / APP / PROCESS", "CPU/#", "MEASURE / HOST / ATTRIBUTION", "MEM"}
		weights = []int{2, 28, 8, 40, 12}
		m.machineRows(add)

	}
	ids := map[string]bool{}
	for _, r := range rows {
		if r.machine != "" {
			ids[r.machine] = true
		}
	}
	if len(ids) > 1 {
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
			if titles[j] == "REASON" && r.inbox != nil {
				cells[i][j] = view.StateStyle(r.inbox.Kind, m.opts.NoColor).Render(cells[i][j])
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
	// Retain scrolling within a list, including when clicking a visible row.
	// Column-count changes require clearing old cells before changing columns.
	if len(m.table.Columns()) != len(columns) {
		m.table = table.New(table.WithFocused(true))
	}
	m.table.SetStyles(styles)
	m.table.SetColumns(columns)
	m.table.SetRows(cells)
	m.table.SetWidth(tableWidth)
	m.table.SetHeight(m.bodyHeight())
	if delta := m.cursors[m.tab] - m.table.Cursor(); delta < 0 {
		m.table.MoveUp(-delta)
	} else {
		m.table.MoveDown(delta)
	}
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
	return view.SessionDuration(d)
}

func mapTabNumber(tab int) int {
	switch tab {
	case flowTab:
		return 0
	case 1:
		return 1
	case 4:
		return 2
	}
	return -1
}
