package tui

import (
	"charm.land/lipgloss/v2"
	"encoding/json"
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
const fullAutoWarning = "Full auto runs every action without asking for 30 minutes · m again to confirm"

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
	styles := m.Styles()
	styles.Cursor.Blink = false
	m.SetStyles(styles)
	m.Placeholder = "Ask driga anything…"
	m.Prompt = "│ "
	m.CharLimit = chat.MaxText
	m.ShowLineNumbers = false
	m.SetHeight(3)
	m.SetWidth(76)
	// Enter sends, as in most chat apps; Shift+Enter adds a line, and
	// Alt+Enter where the terminal cannot report Shift+Enter.
	m.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter")
	return m
}
func (m *app) openChat() tea.Cmd {
	if m.tab != chatTab {
		m.chatOrigin = m.tab
		m.chatOriginDetail = m.detail
		m.chatContext = chat.Context{Page: map[int]string{0: "agents", 1: "projects", 2: "inbox", 3: "orphans", 4: "machines", 5: "cleanup", flowTab: "flow", chatTab: "chat"}[m.tab], Machine: m.machineScope, Project: m.opts.Project}
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
	m.chatFullAsk = false
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
	case "alt+y", "alt+n":
		return m.decideChat(msg.String() == "alt+y")
	case "y", "n":
		if !m.chatInput.Focused() {
			return m.decideChat(msg.String() == "y")
		}
	case "m":
		if !m.chatInput.Focused() {
			return m.cycleChatMode()
		}
	case "esc":
		if m.chatActive {
			return m.stopChat()
		}
		m.chatInput.Blur()
		return nil
	case "enter":
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
		oldID, oldMode := m.chatID, m.chatMode
		m.chatGeneration++
		m.chatID = ""
		m.chatMessages = nil
		m.chatActions = nil
		m.chatAppeared = nil
		m.chatMode = chat.Confirm
		m.endChatActivity()
		m.chatEventSeq = 0
		m.chatPolling = true
		return m.readChatAfterClose(oldID, oldMode)
	case "ctrl+h":
		if len(m.chatHistory) > 0 {
			oldID, oldMode := m.chatID, m.chatMode
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
			m.chatActions = nil
			m.chatAppeared = nil
			m.chatMode = chat.Confirm
			m.endChatActivity()
			m.chatEventSeq = 0
			m.chatPolling = true
			return m.readChatAfterClose(oldID, oldMode)
		}
		return nil
	case "ctrl+x":
		m.chatContext = chat.Context{}
		return nil
	case "ctrl+t":
		m.chatTools = !m.chatTools
		return nil
	case "pgup":
		m.scrollChat(1, true)
		return nil
	case "pgdown":
		m.scrollChat(-1, true)
		return nil
	case "ctrl+end":
		m.chatOffset = 0
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
	mode := m.chatMode
	if mode == "" {
		mode = chat.Confirm
	}
	modeStyle := style
	if mode == chat.FullAuto {
		modeStyle = view.TokenStyle("crit", m.opts.NoColor)
	}
	label := string(mode)
	if mode == chat.FullAuto && !m.chatUntil.IsZero() {
		left := max(0, m.chatUntil.Sub(m.opts.Now()))
		label += fmt.Sprintf(" · %dm left", int((left+time.Minute-1)/time.Minute))
	}
	header := style.Render("Chat · Driga") + "\n" + dim.Render("answered by Claude Code") + " · " + modeStyle.Render(label)
	if m.chatEphemeral {
		header += "\n" + dim.Render("Conversations end with this client.")
	}
	if m.chatFullAsk {
		header += "\n" + wrapText(fullAutoWarning, m.width)
	}
	previousBodies := m.chatBodies
	m.chatBodies = make(map[string]chatBodyRender, len(m.chatMessages))
	var lines []string
	tools := 0
	for _, row := range m.chatTranscript() {
		if row.Role == "assistant" && strings.TrimSpace(row.Text) == "" {
			continue
		}
		if row.Role == "action" {
			if row.Status == "pending" {
				continue
			}
			card := truncate(clean(row.Text), m.width)
			for _, a := range m.chatActions {
				if a.ID == row.ID {
					card = m.chatCard(a)
					break
				}
			}
			lines = append(lines, card, "")
			continue
		}
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
		cached := previousBodies[row.ID]
		width := max(1, m.width-4)
		dark := view.HasDarkBackground()
		if cached.text != text || cached.role != row.Role || cached.width != width || cached.noColor != m.opts.NoColor || cached.dark != dark || cached.body == "" {
			cached = chatBodyRender{text: text, role: row.Role, width: width, noColor: m.opts.NoColor, dark: dark}
			if row.Role == "assistant" {
				cached.body = chatMarkdown(text, width, m.opts.NoColor)
			} else {
				cached.body = wrapText(text, width)
			}
		}
		m.chatBodies[row.ID] = cached
		body := cached.body
		header := style.Render(name)
		if row.Role == "assistant" && row.Status == "streaming" {
			// Activity is shown once, on the status line above the composer.
			if row.Text != "" && m.chatActive {
				body = chatStreamingCursor(body, max(1, m.width-4), m.opts.NoColor)
			}
		} else if row.Status != "" {
			status := row.Status
			if status == "queued" && m.pendingChat() != nil {
				status += " · sends after you answer the card"
			}
			header += dim.Render(" · " + status)
		}
		lines = append(lines, header, strings.TrimRight(body, "\n"), "")
	}
	if a := m.pendingChat(); a != nil && m.chatTools {
		// Expanded raw input uses the transcript viewport, so every byte remains
		// reachable with paging while the decision title and keys stay pinned.
		lines = append(lines, dim.Render("Raw JSON · "+clean(a.Tool)), wrapText(markdownClean(string(a.Input)), m.width), "")
	}
	if tools > 0 {
		lines = append(lines, dim.Render(fmt.Sprintf("▸ tools used · %d · ctrl+t expand/collapse", tools)))
	}
	if len(lines) == 0 {
		lines = append(lines, dim.Render("Ask what needs you, why a session is stalled, or what this screen means."))
	}
	context := m.chatContextLine()
	m.chatInput.SetWidth(max(1, m.width-4))
	m.chatInput.SetHeight(3)
	if m.pendingChat() != nil && m.height < 28 {
		m.chatInput.SetHeight(2)
	}
	textStyle := textarea.StyleState{Text: view.TokenStyle("ink", m.opts.NoColor), Placeholder: view.TokenStyle("muted", m.opts.NoColor), Prompt: view.TokenStyle("accent", m.opts.NoColor)}
	m.chatInput.SetStyles(textarea.Styles{Focused: textStyle, Blurred: textStyle})
	inputView := m.chatInput.View()
	if m.opts.NoColor {
		inputView = ansi.Strip(inputView)
	}
	composer := inputView
	if context != "" {
		composer = wrapText(context, m.width) + "\n" + composer
	}
	if a := m.pendingChat(); a != nil {
		available := m.bodyHeight() - lipgloss.Height(header) - lipgloss.Height(composer) - 2
		composer = m.chatCard(*a, available) + "\n" + composer
	}
	h := max(0, m.bodyHeight()-strings.Count(header, "\n")-strings.Count(composer, "\n")-3)
	body := strings.Split(strings.TrimRight(strings.Join(lines, "\n"), "\n"), "\n")
	m.chatPageHeight, m.chatMaxOffset = h, max(0, len(body)-h)
	m.chatOffset = min(max(0, m.chatOffset), m.chatMaxOffset)
	offset := m.chatOffset
	end := len(body) - offset
	start := max(0, end-h)
	separator := strings.Repeat("─", m.width)
	if offset > 0 {
		position := truncate(fmt.Sprintf("Lines %d–%d/%d · ctrl+end latest ", start+1, end, len(body)), m.width)
		separator = dim.Render(position + strings.Repeat("─", max(0, m.width-ansi.StringWidth(position))))
	}
	return header + "\n" + separator + "\n" + strings.Repeat("\n", max(0, h-(end-start))) + strings.Join(body[start:end], "\n") + "\n" + composer
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
			nodes := m.snap.BoardData.Nodes
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
	if len(chips) == 1 {
		return ""
	}
	return strings.Join(chips, " ")
}
func nextChatTick(generation uint64) tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{generation: generation} })
}

// selectTab is shared by normal navigation and Alt+N, including focused inputs.
func (m *app) selectTab(number byte) tea.Cmd {
	tab, ok := map[byte]int{'0': flowTab, '1': 1, '2': 4}[number]
	if !ok {
		return nil
	}
	if !m.tabVisible(tab) {
		return nil
	}
	m.chatInput.Blur()
	m.chatFullAsk = false
	m.confirm = nil
	m.chatAnimating = false
	m.chatSpinner = spinner.New(spinner.WithSpinner(spinner.MiniDot))
	m.filtering = false
	m.filter.Blur()
	m.help = false
	m.tab = tab
	m.detail = false
	m.showAckPending()
	m.rebuild()
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
	if !m.chatFullAsk {
		m.chatStatus = "Ready"
	}
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
	if !m.chatActive || m.pendingChat() != nil || m.chatAnimating || m.tab != chatTab || m.opts.NoColor || m.opts.MotionOff {
		return nil
	}
	m.chatAnimating = true
	return m.chatSpinner.Tick
}

func (m *app) chatActivity() string {
	if a := m.pendingChat(); a != nil {
		return "waiting for your approval · " + clean(a.Tool) + " · " + chatElapsed(m.chatWaitingElapsed(a.ID))
	}
	if m.chatFullAsk {
		return fullAutoWarning
	}
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
	if m.pendingChat() != nil {
		return view.TokenStyle("accent", m.opts.NoColor).Render(text)
	}
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

func (m *app) pendingChat() *chat.Action {
	for i := range m.chatActions {
		if m.chatActions[i].Status == "pending" {
			return &m.chatActions[i]
		}
	}
	return nil
}
func (m *app) decideChat(allow bool) tea.Cmd {
	a := m.pendingChat()
	if a == nil || m.opts.Chat == nil {
		return nil
	}
	client, ctx, id, action, generation := m.opts.Chat, m.opts.Context, m.chatID, a.ID, m.chatGeneration
	return func() tea.Msg { return chatSentMsg{err: client.Decide(ctx, id, action, allow), generation: generation} }
}
func (m *app) cycleChatMode() tea.Cmd {
	if m.opts.Chat == nil || m.chatID == "" {
		return nil
	}
	mode := chat.Auto
	if m.chatMode == chat.Auto {
		mode = chat.FullAuto
	} else if m.chatMode == chat.FullAuto {
		mode = chat.Confirm
	}
	// Full auto runs commands without cards, so it takes a second press.
	if mode == chat.FullAuto && !m.chatFullAsk {
		m.chatFullAsk = true
		m.chatStatus = fullAutoWarning
		return nil
	}
	m.chatFullAsk = false
	client, ctx, id, generation := m.opts.Chat, m.opts.Context, m.chatID, m.chatGeneration
	return func() tea.Msg { return chatSentMsg{err: client.SetMode(ctx, id, mode), generation: generation} }
}

// Keep approvals ahead of the reply they interrupted: the broker accumulates
// post-action text in the same assistant message.
func (m *app) chatTranscript() []store.ChatMessage {
	rows := append([]store.ChatMessage(nil), m.chatMessages...)
	for i := 0; i < len(rows); i++ {
		if rows[i].Role != "action" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if rows[j].Role == "user" {
				break
			}
			if rows[j].Role == "assistant" {
				card := rows[i]
				copy(rows[j+1:i+1], rows[j:i])
				rows[j] = card
				break
			}
		}
	}
	return rows
}

func (m *app) chatCard(a chat.Action, heights ...int) string {
	if a.Status != "pending" {
		parts := []string{a.Status}
		if a.DecidedBy != "" {
			parts = append(parts, clean(a.DecidedBy))
		}
		if a.Result != "" {
			parts = append(parts, clean(a.Result))
		}
		suffix := truncate(" · "+strings.Join(parts, " · "), max(1, m.width-8))
		return view.TokenStyle("tool", m.opts.NoColor).Render(truncate(clean(a.Title), max(1, m.width-ansi.StringWidth(suffix))) + suffix)
	}
	accent, dim := view.TokenStyle("accent", m.opts.NoColor), view.TokenStyle("muted", m.opts.NoColor)
	width := max(1, m.width-4)
	var input struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		Path        string `json:"file_path"`
		Content     string `json:"content"`
		Old         string `json:"old_string"`
		New         string `json:"new_string"`
	}
	_ = json.Unmarshal(a.Input, &input)
	fields := clean(a.Title)
	switch a.Tool {
	case "Bash":
		fields = input.Command
	case "Edit":
		fields = fmt.Sprintf("%s · %d → %d bytes", input.Path, len(input.Old), len(input.New))
	case "Write":
		fields = fmt.Sprintf("%s · %d bytes", input.Path, len(input.Content))
	}
	// Reserve the title, directory and keys even at 80×24. Details consume only
	// spare space; the command remains readable with the raw input disclosure.
	rows := []string{accent.Render(truncate("Approve? · "+clean(a.Tool), width))}
	fields = markdownClean(fields)
	fieldLines := strings.Split(wrapText(fields, width), "\n")
	waiting := 0
	for _, card := range m.chatActions {
		if card.Status == "pending" {
			waiting++
		}
	}
	limit := m.bodyHeight() - 8
	if len(heights) > 0 {
		limit = heights[0]
	}
	fixed := 5 // Title, directory/disclosure, keys, and border.
	if input.Description != "" && a.Tool == "Bash" {
		fixed++
	}

	budget := max(1, limit-fixed)
	if len(fieldLines) > budget {
		fieldLines = append(fieldLines[:budget-1], truncate(fieldLines[budget-1], max(1, width-1))+"…")
	}
	rows = append(rows, fieldLines...)
	if input.Description != "" && a.Tool == "Bash" {
		rows = append(rows, dim.Render(truncate(clean(input.Description), width)))
	}
	rows = append(rows, dim.Render(truncate("in "+clean(a.Directory), max(1, width-18))+" · ctrl+t details"))
	if waiting > 1 {
		count := fmt.Sprintf(" · +%d more waiting", waiting-1)
		rows[0] = accent.Render(truncate("Approve? · "+clean(a.Tool), max(1, width-ansi.StringWidth(count)))) + dim.Render(count)
	}
	rows = append(rows, accent.Render(truncate("y allow · n deny · alt+y/alt+n while typing", width)))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(width + 4)
	if !m.opts.NoColor {
		box = box.BorderForeground(view.ThemeColor("accent"))
	}
	return box.Render(strings.Join(rows, "\n"))
}

func (m *app) chatWaitingElapsed(id string) time.Duration {
	if since, ok := m.chatAppeared[id]; ok {
		return m.opts.Now().Sub(since)
	}
	return 0
}

// Observe arrival separately from animation so NoColor and MotionOff still ring.
func (m *app) noticeChatActions() tea.Cmd {
	if m.chatAppeared == nil {
		m.chatAppeared = make(map[string]time.Time)
	}
	ring := false
	for _, a := range m.chatActions {
		if a.Status == "pending" {
			if _, seen := m.chatAppeared[a.ID]; !seen {
				m.chatAppeared[a.ID] = m.opts.Now()
				ring = true
			}
		}
	}
	if m.pendingChat() != nil {
		m.chatAnimating = false
	}
	if ring {
		return tea.Raw("\a")
	}
	return nil
}

func (m *app) readChatAfterClose(id string, mode chat.Mode) tea.Cmd {
	read := m.readChat()
	if mode != chat.FullAuto || id == "" || m.opts.Chat == nil {
		return read
	}
	client, ctx, generation := m.opts.Chat, m.opts.Context, m.chatGeneration
	return func() tea.Msg {
		if err := client.SetMode(ctx, id, chat.Confirm); err != nil {
			return chatStateMsg{generation: generation, err: err}
		}
		return read()
	}
}

func (m *app) scrollChat(delta int, page bool) {
	m.chatView() // Measure the actual transcript after headers and composer.
	if page {
		delta *= max(1, m.chatPageHeight-1)
	}
	m.chatOffset = min(max(0, m.chatOffset+delta), m.chatMaxOffset)
}

// Only bodies are cached: elapsed time, streaming cursors and decisions stay live.
// Rebuilding the map each frame retires messages that left the bounded transcript.
type chatBodyRender struct {
	text, role, body string
	width            int
	noColor, dark    bool
}

// chatPanelView keeps the current destination visible beside contextual Chat.
// Narrow terminals devote the available reading width to the panel.
func (m *app) chatPanelView() string {
	if m.width < 120 {
		return m.chatView()
	}
	width := m.width
	panelWidth := min(80, max(48, width/2))
	m.width = panelWidth
	right := m.chatView()
	m.width = width - panelWidth - 3
	tab := m.tab
	m.tab = m.chatOrigin
	left := m.table.View()
	if m.tab == flowTab {
		left, _ = m.boardLayout()
	} else if m.tab == 4 {
		left = m.machineTable(left)
	}
	m.tab = tab
	m.width = width
	return lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(width-panelWidth-3).Render(left), " │ ", lipgloss.NewStyle().Width(panelWidth).Render(right))
}
