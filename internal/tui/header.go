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
	cleaned := make([]string, len(parts))
	for i, part := range parts {
		cleaned[i] = clean(part)
	}
	return styledPack(cleaned, width, separator)
}
func wrapText(text string, width int) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, ansi.Wrap(clean(line), max(1, width), ""))
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
	if m.tab == flowTab {
		m.syncBoard()
		identityParts = append(identityParts, fmt.Sprintf("%d waiting", len(m.board.Needs)))
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
	health := []string{headerGauge("mem", sample.MemUsed, sample.MemTotal, m.opts.NoColor), headerGauge("swap", sample.SwapUsed, sample.SwapTotal, m.opts.NoColor), "load " + view.TokenStyle(loadToken, m.opts.NoColor).Render(view.FormatLoad(sample.Load1, "%.1f")) + fmt.Sprintf("/%d", sample.Cores), "disk " + view.HumanBytes(sample.DiskFree)}
	identity = strings.Join(identityParts, " · ")
	// One line at every width: drop health readings from the least important
	// (disk, then load, then swap) before the header would wrap. Machines
	// shows all of them.
	for n := len(health); n > 0; n-- {
		right := strings.Join(health[:n], "   ")
		if lipgloss.Width(identity)+lipgloss.Width(right)+2 <= m.width {
			return identity + strings.Repeat(" ", m.width-lipgloss.Width(identity)-lipgloss.Width(right)) + right
		}
	}
	return styledPack(append(identityParts, health...), m.width, "   ")
}
func (m *app) footer() string {
	if m.help {
		return truncate("↑↓/pgup/pgdown/wheel scroll · ?/esc close", m.width)
	}
	hints := []string{"0 Flow", "M machine", "enter detail", "/ filter", "s sort", "g group by project", "o open terminal"}
	if m.tab == flowTab {
		hints = []string{"m/t/s views", "j/k select", "enter open", "/ find", "tab needs", "J terminal", "c Chat"}
	}
	if m.tmuxTarget() != "" {
		hints = append(hints, "a attach tmux")
	}
	if m.tab == 2 {
		hints = append(hints, "x/space ack")
	}
	if m.tab == 2 {
		if r, ok := m.selected(); ok && r.prompt != nil {
			hints = append(hints, "y allow / n deny")
		}
		hints = append(hints, "X ack all")
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
		// One line, most important first: hints that do not fit are dropped
		// rather than wrapped, so the layout height never changes.
		hints := []string{"c type", "m trust mode", "0-2 tabs", "? help", "q quit"}
		if m.chatInput.Focused() {
			escape := "esc navigate"
			if m.chatActive || m.pendingChat() != nil {
				escape = "esc stop"
			}
			hints = []string{escape, "enter send", "shift+enter new line", "alt+0/1/2 tabs", "alt+↑ edit last", "ctrl+x clear context"}
		}
		if m.pendingChat() != nil {
			hint := "y/n allow/deny"
			if m.chatInput.Focused() {
				hint = "alt+y/alt+n allow/deny"
			}
			hints = append([]string{hint}, hints...)
		}
		line := ""
		for _, hint := range hints {
			next := hint
			if line != "" {
				next = line + " · " + hint
			}
			if ansi.StringWidth(next) > m.width {
				break
			}
			line = next
		}
		return view.TokenStyle("muted", m.opts.NoColor).Render(line)
	}
	hints = append(hints, "esc back", "? help", "q quit")
	if m.tab == 4 || m.tab == 3 || m.tab == 5 {
		hints = append([]string{"h health", "p reclaim processes", "d reclaim disk"}, hints...)
	}
	if m.tab == 0 || m.tab == 2 {
		hints = append([]string{"m/t/s Flow views", "h activity"}, hints...)
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
	measure := func() int {
		return lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + lipgloss.Height(m.footer()) + 3
	}
	overhead := measure()
	// Plain selection brackets and colored state counts can wrap chrome at
	// different boundaries. Reserve the larger frame for identical board geometry.
	if m.tab == flowTab {
		original := m.opts.NoColor
		m.opts.NoColor = !original
		overhead = max(overhead, measure())
		m.opts.NoColor = original
	}
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
	lines = append(lines, "~reclaim "+view.HumanBytes(o.RSSTotal)+" (RSS estimate)", "driga orphans kill "+o.ID)
	if c.kind == "force" {
		lines[0] = "Really SIGKILL survivors? [y/N]"
		lines[len(lines)-1] += " --force"
	}
	return wrapText(strings.Join(lines, "\n"), m.width)
}
func (m *app) View() tea.View {
	if m.width <= 0 {
		return tea.NewView("")
	}
	body := m.table.View()
	if m.tab == 4 {
		body = m.machineTable(body)
	}
	if len(m.rows) == 0 {
		body = m.emptyText()
	}
	if m.tab == flowTab {
		lines := strings.Split(m.flowView(), "\n")
		offset := min(m.flowOffset, max(0, len(lines)-m.bodyHeight()))
		body = strings.Join(lines[offset:], "\n")
	} else if m.splitDetail() {
		body = m.agentsSplitView()
	}
	if !m.loaded {
		body = "Loading…"
		if _, ok := m.src.(*LiveSnapshotSource); ok {
			body = "Collecting a snapshot (the Driga service is not running)…"
		}
	}
	if m.tab == chatTab {
		body = m.chatPanelView()
	}
	if m.detail {
		body = m.viewport.View()
	}
	if m.help {
		body = m.helpText()
		lines := strings.Split(body, "\n")
		m.helpOffset = min(m.helpOffset, max(0, len(lines)-m.bodyHeight()))
		body = strings.Join(lines[m.helpOffset:], "\n")
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
	status := truncate(clean(m.status), m.width)
	if m.busy && m.tab != chatTab {
		status = truncate(clean("working… · "+m.status), m.width)
	}
	if m.tab == chatTab {
		status = m.chatStatusLine()
	} else if m.filter.Value() != "" && !m.help && !m.detail && m.confirm == nil {
		status = "Filter: " + truncate(clean(m.filter.Value()), max(1, m.width-20)) + " · esc clear"
	}
	content := m.header() + "\n" + m.tabBar() + "\n" + strings.Repeat("─", m.width) + "\n" + body + "\n" + status + "\n" + m.footer()
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

func (m *app) emptyText() string {
	if m.filter.Value() != "" {
		return "No matching items · esc clear filter"
	}
	text := map[int]string{
		0: "No agents · start an agent session to see its state here.",
		1: "No projects · collected agent sessions appear here by project.",
		2: "No inbox items · prompts and completions appear here.",
		3: "No orphan processes · idle helper trees appear here for review.",
		4: "No machine samples · waiting for collection.",
		5: "No cleanup candidates · r refresh candidates.",
	}
	return wrapText(text[m.tab], m.width)
}

func (m *app) helpText() string {
	body := wrapText("0 Flow · 1 Projects · 2 Machines · c Chat panel · Esc closes panel\nFlow: m Map · t Timeline · s Sessions · h activity / return\nMap and Timeline use a project tree in the terminal. Tab focuses Needs you.\nj/k or arrows select · Enter opens · / find · Esc returns\ny/n answer explicitly focused prompt after review · J Terminal · K Stop, then Enter\nMachines: h health · p reclaim processes · d reclaim disk\nSessions: S sort · right/left expand families\nChat: Enter sends · Shift+Enter new line · Ctrl+n new · Ctrl+h history\nChat: y/n allow/deny · Alt+y/Alt+n while typing · m trust mode\nAlt+0/1/2 switches tabs while typing · Ctrl+x clears Chat context\n? help · q / ctrl+c quit", m.width)
	if m.tmuxTarget() != "" {
		body += "\na attach tmux"
	}
	_, notes := m.intake()
	if len(notes) > 0 {
		body += "\n" + wrapText(strings.Join(notes, "\n"), m.width)
	}
	return body
}
func (m *app) scrollHelp(delta int) {
	limit := max(0, len(strings.Split(m.helpText(), "\n"))-m.bodyHeight())
	m.helpOffset = min(max(0, m.helpOffset+delta), limit)
}
