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

func (m *app) header() string {
	s := m.visibleSummary()
	sample := s.Sample
	filled := 0
	if sample.SwapTotal > 0 {
		filled = min(8, int(float64(sample.SwapUsed)/float64(sample.SwapTotal)*8))
	}
	mode := m.snap.Mode
	if mode == "snapshot" {
		mode = "snapshot · no history"
	}
	if mode == "" {
		mode = "loading"
	}
	host := m.snap.Hostname
	if host == "" {
		host = m.snap.Summary.MachineID
	}
	health := []string{fmt.Sprintf("swap %s/%s %s%s", humanBytes(sample.SwapUsed), humanBytes(sample.SwapTotal), strings.Repeat("▇", filled), strings.Repeat("░", 8-filled)), fmt.Sprintf("load %.1f/%d", sample.Load1, sample.Cores)}
	if sample.TS.IsZero() {
		health = []string{"health unavailable"}
	}
	parts := append([]string{"driga · " + host + m.fleetHeader(), mode}, health...)
	return pack(append(parts, fmt.Sprintf("%d agents (%d working, %d waiting, %d stalled?)", s.Agents, s.Working, s.Waiting, s.Stalled), fmt.Sprintf("%d orphans ~%s", s.Orphans, humanBytes(s.Reclaim))), m.width, " · ")
}
func (m *app) footer() string {
	hints := []string{"0 Home", "m machine", "enter detail", "/ filter", "s sort", "g group by project", "o open terminal"}
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
	hints = append(hints, "esc back", "? help", "q quit")
	if m.tab == 5 {
		hints = append([]string{"x clean (confirm)", "r refresh candidates"}, hints...)
	}
	text := pack(hints, m.width, " · ")
	var names []string
	for name, f := range m.snap.Freshness {
		sourceName := name
		if _, tail, ok := strings.Cut(name, "/"); ok {
			sourceName = tail
		}
		if f.Status == "not-yet-receiving" {
			text += "\n" + truncate(clean(name)+": not-yet-receiving", m.width)
			continue
		}
		interval := view.SourceInterval(sourceName, collector.Cadence(0))
		for _, machine := range m.snap.Machines.Rows {
			if name == machine.MachineID {
				interval = 15 * time.Second
			}
		}
		stale := f.Stale || m.snap.Mode != "snapshot" && (f.LastOKAt.IsZero() || m.opts.Now().Sub(f.LastOKAt) > 3*interval)
		if stale {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		f := m.snap.Freshness[name]
		age := time.Duration(f.AgeSeconds) * time.Second
		if !f.LastOKAt.IsZero() {
			age = m.opts.Now().Sub(f.LastOKAt)
		}
		line := truncate(fmt.Sprintf("(stale %s %s)", clean(name), duration(age)), m.width)
		if !m.opts.NoColor {
			line = lipgloss.NewStyle().Faint(true).Render(line)
		}
		text += "\n" + line
	}
	return text
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
	} else if m.splitDetail() {
		body = m.agentsSplitView()
	}
	if !m.loaded {
		body = "Loading…"
		if _, ok := m.src.(*LiveSnapshotSource); ok {
			body = "Collecting a snapshot (no collector running)…"
		}
	}
	if m.detail {
		body = m.viewport.View()
	}
	if m.help {
		body = wrapText("0 Home · 1–6 tabs · ↑/↓ or j/k move · enter detail · esc back\nm cycle machine scope · / filter · enter apply · esc clear · s sort: state, age, mem, project · S summarize selected project\ng group by project · →/← expand/collapse families and worktrees\nx/space acknowledge Needs you / Inbox · X acknowledge all Inbox (confirm)\npgup/pgdown scroll Home · k kill orphan (confirm) · K force (two confirmations)\no open terminal · ? close help · q / ctrl+c quit", m.width)
		if m.tmuxTarget() != "" {
			body += "\na attach tmux"
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
	if m.busy {
		status = "working… · " + status
	}
	content := m.header() + "\n" + m.tabBar() + "\n" + strings.Repeat("─", m.width) + "\n" + body + "\n" + truncate(clean(status), m.width) + "\n" + m.footer()
	lines = strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = truncate(line, m.width)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
