package tui

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/inbox"
)

func (m *app) jumpInbox(r tabRow) tea.Cmd {
	j := r.inbox.Jump
	m.status = j.Command
	if j.Cwd != "" {
		m.status = j.Cwd + " · " + m.status
	}
	if m.remote(r.machine) {
		m.status = m.machineName(r.machine) + ": " + m.status
		return nil
	}
	server, _, _ := strings.Cut(m.opts.Tmux, ",")
	var args []string
	if j.Kind == "tmux" && j.Server != "" && server == j.Server {
		args = []string{"tmux", "-S", j.Server, "switch-client", "-t", j.Target}
	}
	if j.Kind == "terminal" && m.opts.JumpTerminal && runtime.GOOS == "darwin" {
		if script := inbox.TerminalActivateScript(j.App); script != "" {
			args = []string{"osascript", "-e", script, j.Target}
		}
	}
	if len(args) == 0 {
		return nil
	}
	run, ctx := m.opts.Exec, m.opts.Context
	status := strings.Join(args, " ")
	m.busy = true
	return func() tea.Msg {
		var err error
		if run != nil {
			err = run(args)
		} else {
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			err = exec.CommandContext(ctx, args[0], args[1:]...).Run()
		}
		return actionMsg{status: status, err: err}
	}
}
