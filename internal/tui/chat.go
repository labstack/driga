package tui

import (
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
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
	err        error
	text       string
	generation uint64
}
type chatTickMsg struct{ generation uint64 }

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
	m.confirm = nil
	m.filter.Blur()
	m.detail = false
	m.filtering = false
	m.help = false
	focus := m.chatInput.Focus()
	m.rebuild()
	if m.opts.Chat == nil {
		m.chatStatus = "Chat unavailable"
		return focus
	}
	if m.chatPolling {
		return tea.Batch(focus, m.animateChat())
	}
	m.chatPolling = true
	return tea.Batch(m.readChat(), focus, m.animateChat())
}
func (m *app) readChat() tea.Cmd {
	client, ctx, id, generation, after := m.opts.Chat, m.opts.Context, m.chatID, m.chatGeneration, m.chatEventSeq
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
		state, err := client.Read(ctx, id, after)
		return chatStateMsg{generation: generation, state: state, history: rows, err: err}
	}
}
func (m *app) chatKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if m.chatActive {
			return m.stopChat()
		}
		m.chatInput.Blur()
		return nil
	case "ctrl+enter", "super+enter", "alt+enter":
		text := m.chatInput.Value()
		if strings.TrimSpace(text) == "" || m.chatID == "" || m.opts.Chat == nil {
			return nil
		}
		m.chatInput.SetValue("")
		client, ctx, id, scope, generation := m.opts.Chat, m.opts.Context, m.chatID, m.chatContext, m.chatGeneration
		m.chatStatus = "Sending…"
		return func() tea.Msg {
			return chatSentMsg{err: client.Send(ctx, id, text, scope), text: text, generation: generation}
		}
	case "ctrl+n":
		m.chatGeneration++
		m.chatID = ""
		m.chatMessages = nil
		m.endChatActivity()
		m.chatEventSeq = 0
		m.chatPolling = true
		return m.readChat()
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
			m.endChatActivity()
			m.chatEventSeq = 0
			m.chatPolling = true
			return m.readChat()
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
		return m.selectTab('0')
	}
	var cmd tea.Cmd
	m.chatInput, cmd = m.chatInput.Update(msg)
	return cmd
}
func (m *app) stopChat() tea.Cmd {
	if m.opts.Chat == nil || m.chatID == "" {
		return nil
	}
	client, ctx, id, generation := m.opts.Chat, m.opts.Context, m.chatID, m.chatGeneration
	return func() tea.Msg { return chatSentMsg{err: client.Stop(ctx, id), generation: generation} }
}
func (m *app) chatView() string {
	style := view.TokenStyle("accent", m.opts.NoColor)
	dim := view.TokenStyle("muted", m.opts.NoColor)
	header := style.Render("Chat · Driga") + " · Codex/Copilot coming\n" + dim.Render("answered by Claude Code") + "\n" + dim.Render("[Confirm · Auto · Full auto] disabled · actions arrive in the next release") + "\n" + dim.Render("Read-only · driga tools only")
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
		name := "Driga"
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
		var body string
		if row.Role == "assistant" {
			body = chatMarkdown(text, max(1, m.width-4), m.opts.NoColor)
		} else {
			body = wrapText(text, max(1, m.width-4))
		}
		header := style.Render(name)
		if row.Role == "assistant" && row.Status == "streaming" {
			// Activity is shown once, on the status line above the composer.
			if row.Text != "" && m.chatActive {
				body = chatStreamingCursor(body, max(1, m.width-4), m.opts.NoColor)
			}
		} else if row.Status != "" {
			header += dim.Render(" · " + row.Status)
		}
		lines = append(lines, header, body, "")
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
	composer := wrapText(context, m.width) + "\n" + inputView
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
		chips = append(chips, "[project · "+clean(pathx.RecordedBase(project))+"]")
	}
	return strings.Join(chips, " ")
}
func nextChatTick(generation uint64) tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{generation: generation} })
}

// selectTab is shared by normal navigation and Alt+N, including focused inputs.
func (m *app) selectTab(number byte) tea.Cmd {
	if number == '8' {
		return m.openChat()
	}
	tab := int(number - '1')
	if number == '0' {
		tab = homeTab
	}
	if number == '7' {
		tab = flowTab
	}
	if !m.tabVisible(tab) {
		return nil
	}
	m.chatInput.Blur()
	m.confirm = nil
	m.chatAnimating = false
	m.chatSpinner = spinner.New(spinner.WithSpinner(spinner.MiniDot))
	m.filtering = false
	m.filter.Blur()
	m.help = false
	if tab == flowTab {
		m.flowID = 0
		m.flowExpanded = false
		m.flowOffset = 0
		m.cursors[flowTab] = 0
		m.flowPulse, m.flowSeen = nil, nil
	}
	m.tab = tab
	m.homeOffset = 0
	m.detail = false
	m.showAckPending()
	m.rebuild()
	if tab == flowTab {
		return m.loadFlow()
	}
	if tab == 5 && m.opts.CleanupList != nil && !m.busy {
		return m.loadCleanup()
	}
	return nil
}

func (m *app) endChatActivity() {
	m.chatActive = false
	m.chatStarted = time.Time{}
	m.chatReplyID = ""
	m.chatTool = ""
	m.chatAnimating = false
	// Invalidate any outstanding spinner frame from the previous turn.
	m.chatSpinner = spinner.New(spinner.WithSpinner(spinner.MiniDot))
}

func (m *app) updateChatActivity(state chat.State) {
	m.chatStatus = "Ready"
	if !state.Active {
		m.endChatActivity()
		return
	}
	if !m.chatActive {
		m.chatStarted = m.opts.Now()
	}
	m.chatActive = true
	for _, row := range state.Messages {
		if row.Role == "assistant" && row.Status == "streaming" && row.ID != m.chatReplyID {
			m.chatReplyID = row.ID
			m.chatStarted = m.opts.Now()
			if !row.CreatedAt.IsZero() {
				m.chatStarted = row.CreatedAt
			}
			m.chatTool = ""
		}
	}
	if state.Reset {
		m.chatTool = ""
		for _, row := range state.Messages {
			if row.ID == m.chatReplyID {
				m.chatTool = ""
			}
			if row.Role == "tool" {
				m.chatTool = row.Text
			}
		}
	}
	for _, event := range state.Events {
		if event.Seq <= m.chatEventSeq {
			continue
		}
		m.chatEventSeq = event.Seq
		if event.Kind == "delta" {
			m.chatTool = ""
		}
		if event.Message != nil {
			if event.Message.Role == "assistant" {
				m.chatTool = ""
			}
			if event.Message.Role == "tool" {
				m.chatTool = event.Message.Text
			}
		}
	}
}

func (m *app) animateChat() tea.Cmd {
	if !m.chatActive || m.chatAnimating || m.tab != chatTab || m.opts.NoColor || m.opts.MotionOff {
		return nil
	}
	m.chatAnimating = true
	return m.chatSpinner.Tick
}

func (m *app) chatActivity() string {
	if !m.chatActive {
		return m.chatStatus
	}
	label := "thinking…"
	for _, row := range m.chatMessages {
		if row.Role == "assistant" && row.Status == "streaming" && row.Text != "" {
			label = "writing…"
		}
	}
	if m.chatTool != "" {
		name := strings.TrimPrefix(m.chatTool, "mcp__driga__")
		name = strings.TrimPrefix(strings.TrimPrefix(name, "list_"), "get_")
		label = "reading " + strings.ReplaceAll(clean(name), "_", " ") + "…"
	}
	return m.chatFrame() + " " + label + " · " + chatElapsed(m.opts.Now().Sub(m.chatStarted))
}

// chatFrame is the plain spinner frame: status text is cleaned of control
// characters before it is drawn, so colour is applied only afterwards.
func (m *app) chatFrame() string {
	if m.opts.NoColor || m.opts.MotionOff {
		return "…"
	}
	m.chatSpinner.Style = lipgloss.NewStyle()
	return m.chatSpinner.View()
}

// chatStatusLine colours the spinner of an active reply after cleaning.
func (m *app) chatStatusLine() string {
	text := truncate(clean(m.chatActivity()), m.width)
	if !m.chatActive || m.opts.NoColor {
		return text
	}
	frame := m.chatFrame()
	if rest, ok := strings.CutPrefix(text, frame); ok {
		return view.TokenStyle("accent", false).Render(frame) + view.TokenStyle("muted", false).Render(rest)
	}
	return text
}

func chatElapsed(elapsed time.Duration) string {
	seconds := max(0, int(elapsed/time.Second))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
}

func chatStreamingCursor(body string, width int, noColor bool) string {
	cursor := view.TokenStyle("accent", noColor).Render("▍")
	last := body[strings.LastIndex(body, "\n")+1:]
	if ansi.StringWidth(last) >= width {
		return body + "\n" + cursor
	}
	return body + cursor
}
