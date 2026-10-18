package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

type tabRow struct {
	key     string
	cells   []string
	agent   int64
	project int64
	cwd     string
	inbox   *model.InboxItem
	orphan  *model.Orphan
}

func (m *app) tabVisible(tab int) bool {
	if m.opts.Project == "" {
		return true
	}
	switch tab {
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
	}
	return false
}
func (m *app) tabBar() string {
	names := []string{fmt.Sprintf("Agents(%d)", m.snap.Summary.Agents), fmt.Sprintf("Projects(%d)", len(m.snap.Projects.Rows)), fmt.Sprintf("Inbox(%d)", len(m.snap.Inbox.Rows)), fmt.Sprintf("Orphans(%d)", len(m.snap.Orphans.Rows)), "Machine"}
	// Keep stable key numbers even when project scope hides a tab.
	var tabs []string
	for i, name := range names {
		if !m.tabVisible(i) {
			continue
		}
		label := fmt.Sprintf("%d %s", i+1, name)
		if i == m.tab {
			label = "[" + label + "]"
		}
		tabs = append(tabs, label)
	}
	return pack(tabs, m.width, "  ")
}

func (m *app) rebuild() {
	if !m.tabVisible(m.tab) {
		for i := 0; i < 5; i++ {
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
		if query != "" && !strings.Contains(strings.ToLower(strings.Join(r.cells, " ")), query) {
			return
		}
		rows = append(rows, r)
	}
	switch m.tab {
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
				add(tabRow{key: key, agent: r.ID, cells: []string{"", r.Label, strings.Repeat(" ", depth) + mark + r.Adapter, r.Project, r.Branch, title, duration(r.Age), humanBytes(r.Mem)}})
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
			}
			name := r.Status.Repo.Name
			if r.Worktrees > 0 {
				mark := "▸ "
				if m.expanded[key] {
					mark = "▾ "
				}
				name = mark + name
			}
			add(tabRow{key: key, project: r.Status.Repo.ID, cwd: r.Status.Repo.Path, cells: []string{"", name, r.Status.Activity, strconv.Itoa(r.Status.LiveAgents), strconv.Itoa(r.Dirty), strconv.Itoa(r.Unpushed), strconv.Itoa(r.Worktrees), where}})
			if m.expanded[key] {
				for _, wt := range m.snap.ProjectDetails[r.Status.Repo.ID].WorktreeRows {
					add(tabRow{key: key, project: r.Status.Repo.ID, cwd: wt.Path, cells: []string{"", " ↳ " + wt.Branch, "worktree", "", strconv.Itoa(wt.Dirty + wt.Untracked), strconv.Itoa(wt.Ahead), "", wt.Path}})
				}
			}
		}
	case 2:
		titles = []string{"", "ID", "KIND", "AGE", "PROJECT", "SUMMARY"}
		weights = []int{2, 16, 9, 6, 12, 35}
		for _, r := range m.snap.Inbox.Rows {
			item := r.InboxItem
			add(tabRow{key: "inbox:" + r.ID, agent: r.SessionID, inbox: &item, cells: []string{"", r.ID, r.Kind, duration(r.Age), r.Project, r.Summary}})
		}
	case 3:
		titles = []string{"", "ID", "AGE", "SIGNATURE", "PROCS", "~RECLAIM", "CWD", "VERIFIED", "EVIDENCE"}
		weights = []int{2, 14, 6, 12, 6, 10, 14, 9, 20}
		for _, r := range m.snap.Orphans.Rows {
			o := r.Orphan
			age := duration(r.Age)
			if o.Young {
				age = "young"
			}
			add(tabRow{key: "orphan:" + o.ID, cwd: o.Cwd, orphan: &o, cells: []string{"", o.ID, age, o.Signature, strconv.Itoa(o.ProcCount), "~" + humanBytes(o.RSSTotal), o.Cwd, strconv.FormatBool(o.Verified), strings.Join(o.Evidence, "; ")}})
		}
	case 4:
		titles = []string{"", "METRIC / AGENT", "VALUE / ROLE", "MEM"}
		weights = []int{2, 30, 28, 20}
		s := m.snap.Machine.Sample
		for _, r := range [][3]string{{"memory", humanBytes(s.MemUsed) + "/" + humanBytes(s.MemTotal), ""}, {"compressed", humanBytes(s.MemCompressed), ""}, {"swap", humanBytes(s.SwapUsed) + "/" + humanBytes(s.SwapTotal), ""}, {"load (1/5/15)", fmt.Sprintf("%.1f / %.1f / %.1f", s.Load1, s.Load5, s.Load15), ""}, {"cores", strconv.Itoa(s.Cores), ""}, {"disk free", humanBytes(s.DiskFree), ""}, {"pressure free", fmt.Sprintf("%.1f", s.PressureFree), ""}} {
			add(tabRow{cells: []string{"", r[0], r[1], r[2]}})
		}
		for _, r := range m.snap.Machine.Memory {
			add(tabRow{cells: []string{"", r.Agent, r.Role, humanBytes(r.RSS)}})
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
	available := max(1, m.width-len(titles))
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
	m.table.SetWidth(m.width)
	m.table.SetHeight(m.bodyHeight())
	m.table.SetCursor(m.cursors[m.tab])
	m.filter.SetWidth(max(1, m.width-2))
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.bodyHeight())
	if m.detail {
		m.viewport.SetContent(wrapText(m.detailText, m.width))
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
