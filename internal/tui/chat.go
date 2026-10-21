package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

const chatTab = 8

type chatStateMsg struct {
	generation uint64
	state      chat.State
	history    []store.Conversation
	err        error
}
type chatSentMsg struct {
	err  error
	text string
}
type chatTickMsg struct{}

func newChatInput() textarea.Model {
	m := textarea.New()
	m.Placeholder = "Ask driga anything…"
	m.Prompt = "│ "
	m.CharLimit = chat.MaxText
	m.ShowLineNumbers = false
	m.SetHeight(3)
	m.SetWidth(76)
	return m
}
func (m *app) openChat() tea.Cmd {
	if m.tab != chatTab {
		m.chatContext = chat.Context{Page: map[int]string{homeTab: "home", 0: "agents", 1: "projects", 2: "inbox", 3: "orphans", 4: "machines", 5: "cleanup", flowTab: "flow", chatTab: "chat"}[m.tab], Machine: m.machineScope, Project: m.opts.Project}
		if m.tab == flowTab {
			if n := m.flowSelected(); n != nil {
				m.chatContext.SessionID = n.ID
				if n.MachineID != "" {
					m.chatContext.Machine = n.MachineID
				}
			}
		} else if r, ok := m.selected(); ok {
			m.chatContext.SessionID = r.agent
			if r.machine != "" {
				m.chatContext.Machine = r.machine
			}
			if r.agent > 0 {
				m.chatContext.Project = m.selectedCwd()
			}
			if r.project > 0 && r.cwd != "" {
				m.chatContext.Project = r.cwd
			}
		}
	}
	m.tab = chatTab
	m.detail = false
	m.filtering = false
	m.help = false
	m.chatInput.Focus()
	m.rebuild()
	if m.opts.Chat == nil {
		m.chatStatus = "Chat unavailable"
		return nil
	}
	if m.chatPolling {
		return nil
	}
	m.chatPolling = true
	return m.readChat()
}
func (m *app) readChat() tea.Cmd {
	client, ctx, id, generation := m.opts.Chat, m.opts.Context, m.chatID, m.chatGeneration
	return func() tea.Msg {
		rows, err := client.List(ctx)
		if err != nil {
			return chatStateMsg{generation: generation, err: err}
		}
		if id == "" {
			c, err := client.Create(ctx)
			if err != nil {
				return chatStateMsg{generation: generation, err: err}
			}
			id = c.ID
			rows = append([]store.Conversation{c}, rows...)
		}
		state, err := client.Read(ctx, id, 0)
		return chatStateMsg{generation: generation, state: state, history: rows, err: err}
	}
}
func (m *app) chatKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return m.stopChat()
	case "ctrl+enter", "super+enter", "alt+enter":
		text := m.chatInput.Value()
		if strings.TrimSpace(text) == "" || m.chatID == "" || m.opts.Chat == nil {
			return nil
		}
		m.chatInput.SetValue("")
		client, ctx, id, scope := m.opts.Chat, m.opts.Context, m.chatID, m.chatContext
		m.chatStatus = "Sending…"
		return func() tea.Msg { return chatSentMsg{err: client.Send(ctx, id, text, scope), text: text} }
	case "ctrl+n":
		m.chatGeneration++
		m.chatID = ""
		m.chatMessages = nil
		if !m.chatPolling {
			m.chatPolling = true
			return m.readChat()
		}
		return nil
	case "ctrl+h":
		if len(m.chatHistory) > 0 {
			index := 0
			for i, c := range m.chatHistory {
				if c.ID == m.chatID {
					index = (i + 1) % len(m.chatHistory)
					break
				}
			}
			m.chatGeneration++
			m.chatID = m.chatHistory[index].ID
			m.chatMessages = nil
			if !m.chatPolling {
				m.chatPolling = true
				return m.readChat()
			}
		}
		return nil
	case "ctrl+x":
		m.chatContext = chat.Context{}
		return nil
	case "ctrl+t":
		m.chatTools = !m.chatTools
		return nil
	case "pgup":
		m.chatOffset += max(1, m.bodyHeight()/2)
		return nil
	case "pgdown":
		m.chatOffset = max(0, m.chatOffset-max(1, m.bodyHeight()/2))
		return nil
	case "alt+up":
		if len(m.chatDrafts) > 0 {
			m.chatInput.SetValue(m.chatDrafts[len(m.chatDrafts)-1])
		}
		return nil
	case "ctrl+0":
		m.tab = homeTab
		m.chatInput.Blur()
		m.rebuild()
		return nil
	}
	var cmd tea.Cmd
	m.chatInput, cmd = m.chatInput.Update(msg)
	return cmd
}
func (m *app) stopChat() tea.Cmd {
	if m.opts.Chat == nil || m.chatID == "" {
		return nil
	}
	client, ctx, id := m.opts.Chat, m.opts.Context, m.chatID
	return func() tea.Msg { return chatSentMsg{err: client.Stop(ctx, id)} }
}
func (m *app) chatView() string {
	style := view.TokenStyle("accent", m.opts.NoColor)
	dim := view.TokenStyle("muted", m.opts.NoColor)
	header := style.Render("Chat · Claude") + " · Codex/Copilot coming\n" + dim.Render("[Confirm · Auto · Full auto] disabled · actions arrive in the next release") + "\n" + dim.Render("Read-only · driga tools only · esc Stop")
	if m.chatEphemeral {
		header += "\n" + dim.Render("Conversations end with this client.")
	}
	var lines []string
	tools := 0
	for _, row := range m.chatMessages {
		if row.Role == "tool" {
			tools++
			if m.chatTools {
				lines = append(lines, view.TokenStyle("tool", m.opts.NoColor).Render("  ✓ "+clean(row.Text)))
			}
			continue
		}
		name := "Claude"
		if row.Role == "user" {
			name = "You"
		}
		text := chat.PlainCitations(row.Text)
		if text == "" {
			if row.Status == "streaming" {
				text = "…"
			} else {
				text = "Message body not retained by capture policy."
			}
		}
		lines = append(lines, style.Render(name)+dim.Render(" · "+row.Status), wrapText(text, max(1, m.width-4)), "")
	}
	if tools > 0 {
		lines = append(lines, dim.Render(fmt.Sprintf("▸ tools used · %d · ctrl+t expand/collapse", tools)))
	}
	if len(lines) == 0 {
		lines = append(lines, dim.Render("Ask what needs you, why a session is stalled, or what this screen means."))
	}
	context := m.chatContextLine()
	m.chatInput.SetWidth(max(1, m.width-4))
	textStyle := textarea.StyleState{Text: view.TokenStyle("ink", m.opts.NoColor), Placeholder: view.TokenStyle("muted", m.opts.NoColor), Prompt: view.TokenStyle("accent", m.opts.NoColor)}
	m.chatInput.SetStyles(textarea.Styles{Focused: textStyle, Blurred: textStyle})
	inputView := m.chatInput.View()
	if m.opts.NoColor {
		inputView = ansi.Strip(inputView)
	}
	composer := wrapText(context, m.width) + "\n" + inputView + "\n" + dim.Render("ctrl+enter send · alt+↑ edit last · ctrl+x clear context")
	h := max(1, m.bodyHeight()-strings.Count(header, "\n")-strings.Count(composer, "\n")-3)
	body := strings.Split(strings.Join(lines, "\n"), "\n")
	offset := min(m.chatOffset, max(0, len(body)-h))
	end := len(body) - offset
	start := max(0, end-h)
	return header + "\n" + strings.Repeat("─", m.width) + "\n" + strings.Join(body[start:end], "\n") + strings.Repeat("\n", max(0, h-(end-start))) + "\n" + composer
}

func (m *app) chatContextLine() string {
	scope := m.chatContext
	chips := []string{"Context"}
	if scope.Page != "" {
		chips = append(chips, "["+clean(scope.Page)+"]")
	}
	project, title := scope.Project, ""
	if scope.SessionID > 0 {
		var find func([]view.AgentRow)
		find = func(rows []view.AgentRow) {
			for _, row := range rows {
				if row.ID == scope.SessionID && (scope.Machine == "" || row.MachineID == "" || scope.Machine == row.MachineID || scope.Machine == "local" && !m.remote(row.MachineID)) {
					title = row.Title
					if project == "" {
						project = row.Project
					}
					return
				}
				find(row.Children)
			}
		}
		find(m.snap.Agents.Rows)
		if title == "" {
			nodes := m.flow.Children
			if m.flow.Lead != nil {
				nodes = append([]view.FlowNode{*m.flow.Lead}, nodes...)
			}
			for _, node := range nodes {
				if node.ID == scope.SessionID && (scope.Machine == "" || node.MachineID == "" || scope.Machine == node.MachineID || scope.Machine == "local" && !m.remote(node.MachineID)) {
					title = node.Title
					if project == "" {
						project = node.Project
					}
					break
				}
			}
		}
		if title == "" {
			title = m.snap.AgentDetails[scope.SessionID].Session.Title
		}
		chip := fmt.Sprintf("[session #%d", scope.SessionID)
		if title != "" {
			chip += " · " + truncate(clean(title), 48)
		}
		chips = append(chips, chip+"]")
	}
	if project != "" {
		chips = append(chips, "[project · "+clean(filepath.Base(project))+"]")
	}
	return strings.Join(chips, " ")
}
func nextChatTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{} })
}
