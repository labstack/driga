package tui

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/view"
)

func (m *app) openDetail(r tabRow) {
	var buf bytes.Buffer
	if r.machine != "" {
		fmt.Fprintf(&buf, "Machine: %s · %s · last seen %s ago\n", m.machineName(r.machine), m.machineState(r.machine), m.machineAge(r.machine))
	}
	render := func(data any) {
		if d, ok := data.(view.AgentView); ok {
			buf.WriteString(agentDetail(d, m.width))
			return
		}
		if err := view.Render(&buf, data, view.RenderOpts{NoColor: true, Envelope: m.snap.Envelope}); err != nil {
			fmt.Fprintln(&buf, err)
		}
	}
	switch {
	case r.cleanup != nil:
		fmt.Fprintln(&buf, cleanupDetail(*r.cleanup))
	case r.inbox != nil:
		fmt.Fprintf(&buf, "Inbox %s · %s\n%s\n", clean(r.inbox.ID), clean(r.inbox.Kind), clean(r.inbox.Summary))
		if d, ok := m.snap.AgentDetails[r.agent]; ok {
			render(d)
		}
	case r.orphan != nil:
		render(view.OrphansView{Rows: []view.OrphanRow{{Orphan: *r.orphan}}})
	case r.agent != 0:
		if d, ok := m.snap.AgentDetails[r.agent]; ok {
			render(d)
		}
	case r.project != 0:
		if d, ok := m.snap.ProjectDetails[r.project]; ok {
			render(d)
		}
	default:
		render(m.snap.Machine)
	}
	// Conflicts remain evidence in details, not an extra top-level tab.
	if r.agent != 0 || r.project != 0 {
		fmt.Fprintln(&buf, "Conflicts")
		for _, group := range []struct {
			name string
			rows []view.ConflictRow
		}{{"current", m.snap.Conflicts.Current}, {"recent overlap (last 6h)", m.snap.Conflicts.Recent}} {
			for _, c := range group.rows {
				if r.machine != "" && c.MachineID != r.machine {
					continue
				}
				relevant := r.project != 0
				for _, id := range c.SessionIDs {
					if id == r.agent {
						relevant = true
					}
				}
				if relevant {
					fmt.Fprintf(&buf, "%s · %s · %s · sessions %v · family %t\n", group.name, clean(c.Kind), clean(c.Path), c.SessionIDs, c.Family)
				}
			}
		}
	}
	m.detail = true
	m.detailRow = &r
	lines := strings.Split(buf.String(), "\n")
	for i, line := range lines {
		lines[i] = truncate(clean(line), m.width)
	}
	m.detailText = strings.Join(lines, "\n")
	m.viewport.SetContent(wrapText(m.detailText, m.width))
	m.viewport.GotoTop()
}

func agentDetail(d view.AgentView, width int) string {
	var buf strings.Builder
	line := func(text string) { fmt.Fprintln(&buf, truncate(clean(text), width)) }
	kv := func(key, value string) { line(key + ": " + value) }
	s := d.Session
	kv("ID", strconv.FormatInt(s.ID, 10))
	kv("Agent", s.Adapter)
	kv("State", d.Label)
	kv("Origin", s.Origin)
	kv("Cwd", s.Cwd)
	kv("Branch", s.Branch)
	kv("Title", s.Title)
	kv("Evidence", s.Evidence)
	kv("Observed at", d.ObservedAt.Format(time.RFC3339))
	if s.LastPrompt != "" {
		kv("Last prompt", s.LastPrompt)
	}
	if d.Resume != "" {
		kv("Resume", d.Resume)
	}
	line("Files")
	for _, f := range d.Files {
		kv(f.TS.Format(time.RFC3339), f.Path)
	}
	pidWidth, ppidWidth, memWidth := 3, 4, 3
	for _, p := range d.Procs {
		pidWidth = max(pidWidth, len(strconv.Itoa(int(p.PID))))
		ppidWidth = max(ppidWidth, len(strconv.Itoa(int(p.PPID))))
		memWidth = max(memWidth, len(humanBytes(p.RSS)))
	}
	line(fmt.Sprintf("%-*s  %-*s  %-*s  COMMAND", pidWidth, "PID", ppidWidth, "PPID", memWidth, "MEM"))
	for _, p := range d.Procs {
		command := strings.Join(strings.Fields(clean(p.Cmdline)), " ")
		line(fmt.Sprintf("%-*d  %-*d  %-*s  %s", pidWidth, p.PID, ppidWidth, p.PPID, memWidth, humanBytes(p.RSS), command))
	}
	line("Events")
	for _, e := range d.Events {
		kv(e.TS.Format(time.RFC3339)+" · "+e.Kind, e.Summary)
	}
	return buf.String()
}

// tmuxTarget relies on process ancestry, never the viewer's own TMUX variable.
func (m *app) tmuxTarget() string {
	target, _, ok := m.tmuxAncestry()
	if !ok {
		return ""
	}
	if target == "" {
		return "?"
	}
	return target
}

func (m *app) tmuxAncestry() (string, []int32, bool) {
	r, ok := m.selected()
	if !ok || r.agent == 0 || m.remote(r.machine) {
		return "", nil, false
	}
	s := m.snap.AgentDetails[r.agent].Session
	idx := derive.NewProcIndex(m.snap.Procs)
	proc, ok := idx.Get(s.PID)
	if !ok || s.PID <= 0 || !s.ProcStart.IsZero() && !derive.SameStart(proc.StartTime, s.ProcStart) {
		return "", nil, false
	}
	ancestors := append([]int32{s.PID}, idx.Ancestors(s.PID)...)
	for _, pid := range ancestors[1:] {
		p, _ := idx.Get(pid)
		args := strings.Fields(p.Cmdline)
		if len(args) == 0 || strings.TrimSuffix(filepath.Base(args[0]), ":") != "tmux" {
			continue
		}
		for i := 1; i < len(args)-1; i++ {
			if args[i] == "-s" || args[i] == "-t" {
				return args[i+1], ancestors, true
			}
		}
		return "", ancestors, true
	}
	return "", nil, false
}

func lookupTmux(ctx context.Context, pids []int32) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", "#{session_id}\t#{pane_pid}").Output()
	if err != nil {
		return "", err
	}
	return tmuxSession(string(out), pids)
}

func tmuxSession(output string, pids []int32) (string, error) {
	panes := map[int32]string{}
	for _, line := range strings.Split(output, "\n") {
		session, pid, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(pid, 10, 32)
		if err == nil {
			panes[int32(n)] = session
		}
	}
	for _, pid := range pids {
		if session := panes[pid]; session != "" {
			return session, nil
		}
	}
	return "", fmt.Errorf("tmux session no longer exists")
}

func (m *app) attach() tea.Cmd {
	target, pids, ok := m.tmuxAncestry()
	if !ok {
		return nil
	}
	if target != "" {
		return m.execute([]string{"tmux", "attach", "-t", target}, true)
	}
	opts := m.opts
	m.busy = true
	return func() tea.Msg {
		session, err := opts.LookupTmux(opts.Context, pids)
		if err != nil {
			return actionMsg{status: "tmux attach", err: err}
		}
		args := []string{"tmux", "attach", "-t", session}
		command := strings.Join(args, " ")
		if opts.Exec != nil {
			return actionMsg{status: command, err: opts.Exec(args)}
		}
		return tea.ExecProcess(exec.CommandContext(opts.Context, args[0], args[1:]...), func(err error) tea.Msg { return actionMsg{status: command, err: err} })()
	}
}
