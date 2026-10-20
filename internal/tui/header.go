package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/view"
)

func humanBytes(n uint64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
}
func duration(d time.Duration) string { return max(time.Duration(0), d).Round(time.Second).String() }
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func truncate(s string, width int) string { return ansi.Truncate(s, max(1, width), "…") }
func pack(parts []string, width int, separator string) string {
	var lines []string
	line := ""
	for _, p := range parts {
		p = truncate(clean(p), width)
		if line != "" && lipgloss.Width(line+separator+p) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += separator
		}
		line += p
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
func wrapText(text string, width int) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, ansi.Hardwrap(clean(line), max(1, width), true))
	}
	return strings.Join(lines, "\n")
}

func headerGauge(name string, used, total uint64, noColor bool) string {
	if total == 0 {
		return view.TokenStyle("faint", noColor).Render(name + " —")
	}
	filled := min(6, int(view.GaugePercent(used, total)*6/100))
	style := view.GaugeStyle(used, total, noColor)
	return view.TokenStyle("muted", noColor).Render(name+" ") + style.Render(strings.Repeat("█", filled)) + view.TokenStyle("faint", noColor).Render(strings.Repeat("░", 6-filled)) + " " + style.Render(fmt.Sprintf("%.0f%%", view.GaugePercent(used, total)))
}

func (m *app) header() string {
	sample := m.visibleSummary().Sample
	host := m.snap.Hostname
	if host == "" {
		host = m.snap.Summary.MachineID
	}
	mode := m.snap.Mode
	if mode == "" {
		mode = "loading"
	}
	badge := lipgloss.NewStyle()
	if !m.opts.NoColor {
		badge = badge.Bold(true).Foreground(view.ThemeColor("on-strong")).Background(view.ThemeColor("strong"))
	}
	status := mode
	if mode == "daemon" {
		status = view.TokenStyle("working", m.opts.NoColor).Render("● live") + view.TokenStyle("faint", m.opts.NoColor).Render(" · daemon")
	}
	if mode == "snapshot" {
		status = "snapshot · no history"
	}
	identity := badge.Render(" driga ") + "  " + clean(host) + m.fleetHeader() + " · " + status
	identityParts := []string{identity}
	if m.tab == homeTab || m.tab == flowTab {
		var rows []view.InboxRow
		for _, i := range m.snap.Inbox.Rows {
			if m.machineVisible(i.MachineID) {
				rows = append(rows, i)
			}
		}
		identityParts = append(identityParts, view.WaitingHeader(rows, m.opts.Now()))
	}
	if state, _ := m.intake(); state != "" {
		identityParts = append(identityParts, "intake · "+view.TokenStyle(map[string]string{"receiving": "working", "waiting": "muted", "attention": "warn"}[state], m.opts.NoColor).Render(state))
	}
	if !m.snap.ObservedAt.IsZero() {
		identityParts = append(identityParts, m.snap.ObservedAt.Local().Format("15:04:05"))
	}
	if sample.TS.IsZero() {
		return styledPack(append(identityParts, "health unavailable"), m.width, " · ")
	}
	loadToken := "ok"
	switch view.LoadLevel(sample.Load1, sample.Cores) {
	case "critical":
		loadToken = "crit"
	case "warning":
		loadToken = "warn"
	}
	health := []string{headerGauge("mem", sample.MemUsed, sample.MemTotal, m.opts.NoColor), headerGauge("swap", sample.SwapUsed, sample.SwapTotal, m.opts.NoColor), "load " + view.TokenStyle(loadToken, m.opts.NoColor).Render(fmt.Sprintf("%.1f", sample.Load1)) + fmt.Sprintf("/%d", sample.Cores), "disk " + humanBytes(sample.DiskFree)}
	right := strings.Join(health, "   ")
	identity = strings.Join(identityParts, " · ")
	if lipgloss.Width(identity)+lipgloss.Width(right)+2 <= m.width {
		return identity + strings.Repeat(" ", m.width-lipgloss.Width(identity)-lipgloss.Width(right)) + right
	}
	return styledPack(append(identityParts, health...), m.width, "   ")
}
func (m *app) footer() string {
	hints := []string{"0 Home", "m machine", "enter detail", "/ filter", "s sort", "g group by project", "o open terminal"}
	if m.tab == flowTab {
		hints = []string{"7 flow", "j/k session", "enter jump", "[/] family", "-/+ zoom", "tab needs", "C resume", "c Chat"}
	}
	if m.tab == homeTab {
		hints = []string{"0-8 tabs · c Chat", "↑↓ move", "enter open", "/ filter", "m machine"}
	}
	if m.tmuxTarget() != "" {
		hints = append(hints, "a attach tmux")
	}
	if m.tab == 2 || m.tab == homeTab {
		hints = append(hints, "x/space ack")
	}
	if m.tab == 2 {
		hints = append(hints, "X ack all")
	}
	if m.tab == homeTab {
		hints = append(hints, "pgup/pgdown scroll")
	}
	if m.tab == 1 {
		hints = append([]string{"S summarize"}, hints...)
	}
	if m.tab == 3 {
		hints = append(hints, "k kill", "K force")
		if m.snap.Mode == "snapshot" {
			hints = append(hints, "unverified (snapshot)")
		}
	}
	if m.tab == chatTab {
		hints = []string{"ctrl+0 Home", "ctrl+n new", "ctrl+h history", "pgup/pgdown scroll", "esc Stop"}
	}
	if m.tab == chatTab {
		hints = append(hints, "ctrl+c quit")
	} else {
		hints = append(hints, "esc back", "? help", "q quit")
	}
	if m.tab == 5 {
		hints = append([]string{"x clean (confirm)", "r refresh candidates"}, hints...)
	}
	caps := []string{}
	for _, hint := range hints {
		key, label, _ := strings.Cut(hint, " ")
		style := view.TokenStyle("ink", m.opts.NoColor)
		if !m.opts.NoColor {
			style = style.Background(view.ThemeColor("sunk"))
		}
		caps = append(caps, style.Render(" "+key+" ")+" "+view.TokenStyle("muted", m.opts.NoColor).Render(label))
	}
	text := styledPack(caps, m.width, "  ")
	if m.opts.NoColor {
		text = pack(hints, m.width, " · ")
	}
	return text
}

// intake summarizes source health without treating quiet event sources as stale.
// Detailed ages remain available in help, including between collector refreshes.
func (m *app) intake() (string, []string) {
	state := ""
	notes := []string{}
	names := []string{}
	for name := range m.snap.Freshness {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := m.snap.Freshness[name]
		if state == "" {
			state = "receiving"
		}
		if f.Status == "not_yet_receiving" {
			notes = append(notes, clean(name)+": not-yet-receiving")
			if state != "attention" {
				state = "waiting"
			}
			if f.LastError == "" && !f.Stale {
				continue
			}
		}
		if f.Status == "last_event" {
			notes = append(notes, fmt.Sprintf("%s: last event %s ago", clean(name), duration(m.opts.Now().Sub(f.LastOKAt))))
			if !f.Stale && f.LastError == "" {
				continue
			}
		}
		sourceName := name
		if _, tail, ok := strings.Cut(name, "/"); ok {
			sourceName = tail
		}
		interval := view.SourceInterval(sourceName, collector.Cadence(0))
		for _, machine := range m.snap.Machines.Rows {
			if name == machine.MachineID {
				interval = 15 * time.Second
			}
		}
		stale := f.Stale || m.snap.Mode != "snapshot" && (f.LastOKAt.IsZero() || m.opts.Now().Sub(f.LastOKAt) > 3*interval)
		if stale {
			state = "attention"
			age := time.Duration(f.AgeMS) * time.Millisecond
			if !f.LastOKAt.IsZero() {
				age = m.opts.Now().Sub(f.LastOKAt)
			}
			notes = append(notes, fmt.Sprintf("(stale %s %s)", clean(name), duration(age)))
		}
		if f.LastError != "" {
			state = "attention"
			notes = append(notes, clean(name)+": "+clean(f.LastError))
		}
	}
	return state, notes
}

func (m *app) bodyHeight() int {
	overhead := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + lipgloss.Height(m.footer()) + 3
	return max(2, m.height-overhead)
}
func (m *app) confirmationText() string {
	c := m.confirm
	if c.kind == "cleanup" {
		return wrapText("Clean "+c.cleanup.ID+"? [y/N]\n"+cleanupDetail(c.cleanup), m.width)
	}
	if c.kind == "ack-all" {
		return fmt.Sprintf("Acknowledge %d inbox items? [y/N]\ndriga inbox ack --all", len(c.ids))
	}
	var lines []string
	o := c.orphan
	idx := map[int32]string{}
	for _, p := range m.snap.Procs {
		idx[p.PID] = p.Cmdline
	}
	lines = append(lines, "Kill "+o.ID+"? [y/N]", "PID  CMD", fmt.Sprintf("%d  %s", o.RootPID, clean(idx[o.RootPID])))
	for _, pid := range o.PIDs {
		if pid != o.RootPID {
			lines = append(lines, fmt.Sprintf("%d  %s", pid, clean(idx[pid])))
		}
	}
	lines = append(lines, "~reclaim "+humanBytes(o.RSSTotal)+" (RSS estimate)", "driga orphans kill "+o.ID)
	if c.kind == "force" {
		lines[0] = "Really SIGKILL survivors? [y/N]"
		lines[len(lines)-1] += " --force"
	}
	return wrapText(strings.Join(lines, "\n"), m.width)
}
func (m *app) View() tea.View {
	body := m.table.View()
	if len(m.rows) == 0 {
		body = "No items"
	}
	if m.tab == homeTab {
		lines := strings.Split(m.homeView(), "\n")
		offset := min(m.homeOffset, max(0, len(lines)-m.bodyHeight()))
		body = strings.Join(lines[offset:], "\n")
	} else if m.tab == flowTab {
		lines := strings.Split(m.flowView(), "\n")
		offset := min(m.flowOffset, max(0, len(lines)-m.bodyHeight()))
		body = strings.Join(lines[offset:], "\n")
	} else if m.splitDetail() {
		body = m.agentsSplitView()
	}
	if !m.loaded {
		body = "Loading…"
		if _, ok := m.src.(*LiveSnapshotSource); ok {
			body = "Collecting a snapshot (no collector running)…"
		}
	}
	if m.tab == chatTab {
		body = m.chatView()
	}
	if m.detail {
		body = m.viewport.View()
	}
	if m.help {
		body = wrapText("0 Home · 1–7 tabs · ↑/↓ or j/k move · enter detail · esc back\n7 Flow · j/k session · enter jump · [/] family · -/+ zoom · tab needs · C resume · c Chat\nm cycle machine scope · / filter · enter apply · esc clear · s sort: state, age, mem, project · S summarize selected project\ng group by project · →/← expand/collapse families and worktrees\nx/space acknowledge Needs you / Inbox · X acknowledge all Inbox (confirm)\nclick select · click selected row to open · wheel scroll\npgup/pgdown scroll Home · k kill orphan (confirm) · K force (two confirmations)\no open terminal · ? close help · q / ctrl+c quit", m.width)
		if m.tmuxTarget() != "" {
			body += "\na attach tmux"
		}
		_, notes := m.intake()
		if len(notes) > 0 {
			body += "\n" + wrapText(strings.Join(notes, "\n"), m.width)
		}
	}
	if m.confirm != nil {
		body = m.confirmationText()
	}
	if m.filtering {
		body = m.filter.View() + "\n" + body
	}
	lines := strings.Split(body, "\n")
	if len(lines) > m.bodyHeight() {
		lines = lines[:m.bodyHeight()]
	}
	body = strings.Join(lines, "\n")
	status := m.status
	if m.tab == chatTab {
		status = m.chatStatus
	}
	if m.busy && m.tab != chatTab {
		status = "working… · " + status
	}
	content := m.header() + "\n" + m.tabBar() + "\n" + strings.Repeat("─", m.width) + "\n" + body + "\n" + truncate(clean(status), m.width) + "\n" + m.footer()
	lines = strings.Split(content, "\n")
	for i, line := range lines {
		// Normalize before measuring so tabs cannot shift borders or escape in
		// frames assembled from table/viewport output or retained text.
		lines[i] = truncate(strings.ReplaceAll(line, "\t", " "), m.width)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
