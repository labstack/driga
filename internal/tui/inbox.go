package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/chat"
)

func (m *app) jumpInbox(r tabRow) tea.Cmd {
	j := r.inbox.Jump
	m.status = attention.JumpCommand(j)
	if m.remote(r.machine) {
		m.status = m.machineName(r.machine) + ": " + m.status
		return nil
	}
	if j.Kind == "machine" {
		m.tab = 4
		m.detail = false
		m.rebuild()
		return nil
	}
	commands, err := attention.JumpCommands(j, attention.JumpOptions{Tmux: m.opts.Tmux, Terminal: m.opts.JumpTerminal})
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
				cmd := exec.CommandContext(ctx, args[0], args[1:]...)
				attention.ConfigureCommand(cmd)
				err = cmd.Run()
			}
			if err != nil {
				break
			}
		}
		return actionMsg{status: status, err: err}
	}
}

func (m *app) answerPrompt(p chat.Prompt, allow bool) tea.Cmd {
	client, ok := m.opts.Chat.(chat.PromptClient)
	if !ok {
		return nil
	}
	m.busy = true
	ctx := m.opts.Context
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err := client.AnswerPrompt(ctx, p.ID, allow)
		return actionMsg{status: "Prompt answered", err: err}
	}
}

// TUI can revoke authority, but never creates grants.
func (m *app) revokeLatestGrant() tea.Cmd {
	client, ok := m.opts.Chat.(chat.GrantClient)
	if !ok {
		return nil
	}
	for i := len(m.grantHistory) - 1; i >= 0; i-- {
		g := m.grantHistory[i]
		if !g.Revoked {
			ctx := m.opts.Context
			return func() tea.Msg { return actionMsg{status: "Grant revoked", err: client.RevokeGrant(ctx, g.GrantID)} }
		}
	}
	return nil
}

func burstIdentity(g chat.PromptBurst) string {
	var ids []string
	for _, member := range g.Members {
		ids = append(ids, member.ID+":"+member.InputHash)
	}
	return g.ID + "/" + strings.Join(ids, "/")
}
func (m *app) promptArmIdentity(id string) string {
	if g := m.promptBurst(id); g != nil {
		return burstIdentity(*g)
	}
	return id
}
func (m *app) promptBurst(id string) *chat.PromptBurst {
	for _, g := range m.bursts {
		if g.ID == id && len(g.Members) > 1 {
			copy := g
			return &copy
		}
	}
	return nil
}
func (m *app) attentionPrompts() []chat.Prompt {
	out := []chat.Prompt{}
	grouped := map[string]bool{}
	for _, g := range m.bursts {
		if len(g.Members) < 2 {
			continue
		}
		raw, _ := json.Marshal(g.Members)
		out = append(out, chat.Prompt{ID: g.ID, Session: g.Session, Cwd: g.Cwd, Tool: fmt.Sprintf("Allow all %d", len(g.Members)), Input: raw, CreatedAt: g.OpenedAt})
		for _, member := range g.Members {
			grouped[member.ID] = true
		}
	}
	for _, p := range m.prompts {
		if !grouped[p.ID] {
			out = append(out, p)
		}
	}
	return out
}
func (m *app) decidePromptBurst(g chat.PromptBurst) tea.Cmd {
	client, ok := m.opts.Chat.(chat.BurstClient)
	if !ok || m.busy {
		return nil
	}
	members := []string{}
	for _, member := range g.Members {
		members = append(members, member.ID)
	}
	ctx := m.opts.Context
	m.busy = true
	return func() tea.Msg {
		return actionMsg{status: fmt.Sprintf("Answered %d prompts", len(members)), err: client.DecideBurst(ctx, g.ID, members, true)}
	}
}
func (m *app) burstKey(k string) (tea.Cmd, bool) {
	g := m.detailRow.burst
	switch k {
	case "[", "]":
		delta := 1
		if k == "[" {
			delta = -1
		}
		m.burstIndex = min(max(0, m.burstIndex+delta), len(g.Members)-1)
		m.burstAt = m.opts.Now()
		offset := m.viewport.YOffset()
		m.openDetail(*m.detailRow)
		m.viewport.SetYOffset(offset)
		return nil, true
	case "y", "n":
		if m.busy {
			return nil, true
		}
		if m.opts.Now().Sub(m.burstAt) < 600*time.Millisecond {
			m.status = "Review prompt before answering"
			return nil, true
		}
		if k == "y" {
			return m.decidePromptBurst(*g), true
		}
		return m.answerPrompt(g.Members[m.burstIndex].Prompt, false), true
	}
	return nil, false
}
