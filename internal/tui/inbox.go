package tui

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/inbox"
)

func (m *app) jumpInbox(r tabRow) tea.Cmd {
	j := r.inbox.Jump
	m.status = inbox.JumpCommand(j)
	if m.remote(r.machine) {
		m.status = m.machineName(r.machine) + ": " + m.status
		return nil
	}
	commands, err := inbox.JumpCommands(j, inbox.JumpOptions{Tmux: m.opts.Tmux, Terminal: m.opts.JumpTerminal})
	if err != nil {
		m.status = err.Error()
		return nil
	}
	if len(commands) == 0 {
		return nil
	}
	run, ctx := m.opts.Exec, m.opts.Context
	var display []string
	for _, args := range commands {
		display = append(display, strings.Join(args, " "))
	}
	status := strings.Join(display, " && ")
	m.busy = true
	return func() tea.Msg {
		var err error
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		for _, args := range commands {
			if run != nil {
				err = run(args)
			} else {
				err = exec.CommandContext(ctx, args[0], args[1:]...).Run()
			}
			if err != nil {
				break
			}
		}
		return actionMsg{status: status, err: err}
	}
}
