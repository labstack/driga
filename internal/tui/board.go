package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"fmt"
	"github.com/labstack/driga/internal/view"
	"sort"
	"strings"
	"time"
)

func (m *app) syncBoard() {
	previous := ""
	oldRows := m.boardRows()
	if m.boardCursor >= 0 && m.boardCursor < len(oldRows) {
		previous = oldRows[m.boardCursor].Key
	}
	oldNeed := ""
	if m.flowAskCursor >= 0 && m.flowAskCursor < len(m.board.Needs) {
		oldNeed = m.board.Needs[m.flowAskCursor].Key
	}
	data := m.snap.BoardData
	data.Nodes = nil
	for _, n := range m.snap.BoardData.Nodes {
		if m.machineVisible(n.MachineID) {
			data.Nodes = append(data.Nodes, n)
		}
	}
	for _, p := range m.prompts {
		for i := range data.Nodes {
			n := &data.Nodes[i]
			if n.ID == p.SessionID && n.MachineID == m.localMachine() && n.EndedAt == nil {
				n.Waits = append(append([]view.FlowWait(nil), n.Waits...), view.FlowWait{ID: p.ID, StartedAt: p.CreatedAt, Tool: p.Tool, Detail: p.Detail(), Reason: "permission"})
			}
		}
	}
	b := view.Board(data, time.Hour, m.opts.Now())
	prompts := map[string]string{}
	for _, p := range m.prompts {
		prompts[fmt.Sprintf("%s/%d", m.localMachine(), p.SessionID)] = p.ID
	}
	b.WithPrompts(prompts)
	for _, p := range m.prompts {
		for i := range b.Needs {
			if b.Needs[i].PromptID == p.ID {
				b.Needs[i].Command = p.Detail()
			}
		}
	}
	order := map[string]int{}
	for i, k := range m.boardOrder {
		order[k] = i
	}
	groupOrder := map[string]int{}
	for i, k := range m.boardGroups {
		groupOrder[k] = i
	}
	for gi := range b.Groups {
		g := &b.Groups[gi]
		sort.SliceStable(g.Rows, func(i, j int) bool {
			a, aok := order[g.Rows[i].Key]
			c, cok := order[g.Rows[j].Key]
			if aok != cok {
				return aok
			}
			return aok && a < c
		})
	}
	sort.SliceStable(b.Groups, func(i, j int) bool {
		a, aok := groupOrder[b.Groups[i].Key]
		c, cok := groupOrder[b.Groups[j].Key]
		if aok != cok {
			return aok
		}
		return aok && a < c
	})
	m.boardPinned = previous
	m.board = b
	for i, r := range m.boardRows() {
		if r.Key == previous {
			m.boardCursor = i
			break
		}
	}
	for i, r := range b.Needs {
		if r.Key == oldNeed {
			m.flowAskCursor = i
			break
		}
	}
	m.boardOrder = nil
	m.boardGroups = nil
	for _, g := range b.Groups {
		m.boardGroups = append(m.boardGroups, g.Key)
		for _, r := range g.Rows {
			m.boardOrder = append(m.boardOrder, r.Key)
		}
	}
}
func (m *app) boardAccept(r view.BoardRow) bool {
	if !m.machineVisible(r.Node.MachineID) {
		return false
	}

	states := []string{"", "waiting", "working", "stalled"}
	if m.boardFilter > 0 && r.State != states[m.boardFilter] {
		return false
	}
	q := strings.ToLower(m.filter.Value())
	return q == "" || strings.Contains(strings.ToLower(r.Title+" "+r.Node.Project), q)
}
func (m *app) boardRows() []view.BoardRow {
	rows := []view.BoardRow{}
	for _, g := range m.board.Groups {
		for _, r := range g.Rows {
			if m.boardAccept(r) {
				rows = append(rows, r)
			}
		}
	}
	if m.flowViewMode == "Sessions" {
		sort.SliceStable(rows, func(i, j int) bool {
			switch m.sort {
			case 1:
				return rows[i].State < rows[j].State
			case 2:
				return rows[i].Node.Project < rows[j].Node.Project
			}
			return rows[i].Title < rows[j].Title
		})
	}
	if m.boardMachine {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Node.MachineID < rows[j].Node.MachineID })
	}
	return rows
}
func (m *app) boardSelected() *view.BoardRow {
	m.syncBoard()
	if m.flowAttention && len(m.board.Needs) > 0 {
		m.flowAskCursor = min(m.flowAskCursor, len(m.board.Needs)-1)
		r := m.board.Needs[m.flowAskCursor]
		return &r
	}
	rows := m.boardRows()
	if len(rows) == 0 {
		return nil
	}
	m.boardCursor = max(0, min(m.boardCursor, len(rows)-1))
	r := rows[m.boardCursor]
	return &r
}
func (m *app) boardKey(k string) (tea.Cmd, bool) {
	if m.boardStop != 0 && k != "y" && k != "n" {
		m.boardStop = 0
		m.status = "Cancelled"
	}
	m.syncBoard()
	switch k {
	case "tab", "shift+tab":
		m.flowAttention = !m.flowAttention && len(m.board.Needs) > 0
		m.armBoardPrompt()
		return nil, true
	case "j", "down", "k", "up", "pgdown", "pgup":
		if m.flowAttention && (k == "pgdown" || k == "pgup") {
			delta := max(1, m.bodyHeight()/2)
			if k == "pgup" {
				delta = -delta
			}
			m.flowOffset = max(0, m.flowOffset+delta)
			return nil, true
		}
		m.flowOffset = 0
		d := 1
		if k == "k" || k == "up" || k == "pgup" {
			d = -1
		}
		if k == "pgdown" || k == "pgup" {
			d *= max(1, m.bodyHeight()/2)
		}
		if m.flowAttention {
			m.flowAskCursor = max(0, min(len(m.board.Needs)-1, m.flowAskCursor+d))
		} else {
			m.boardCursor = max(0, min(len(m.boardRows())-1, m.boardCursor+d))
		}
		m.armBoardPrompt()
		return nil, true
	case "m", "t", "s":
		return m.selectFlowView(k), true
	case "S":
		m.sort = (m.sort + 1) % 3
		return nil, true
	case "enter":
		if r := m.boardSelected(); r != nil {
			row := tabRow{machine: r.Node.MachineID, agent: r.Node.ID, key: r.Key, cwd: r.Cwd}
			if m.flowAttention {
				for _, p := range m.prompts {
					if p.ID == r.PromptID {
						copy := p
						row.prompt = &copy
					}
				}
			}
			m.openDetail(row)
			return m.loadSelection(), true
		}
		return nil, true
	case "J":
		return m.jumpFlow(), true
	case "K":
		r := m.boardSelected()
		switch {
		case r == nil || r.State == "finished":
		case !r.Local:
			m.status = "Stop this session on " + m.machineName(r.Node.MachineID)
		case m.opts.SessionAction == nil:
			m.status = fmt.Sprintf("driga agents kill %d · confirm in terminal", r.Node.ID)
		default:
			m.boardStop = r.Node.ID
			m.status = fmt.Sprintf("Stop %q? y stop · n cancel", r.Title)
		}
		return nil, true
	case "y", "n":
		if id := m.boardStop; id != 0 {
			m.boardStop = 0
			if k == "n" {
				m.status = "Cancelled"
				return nil, true
			}
			ctx, run := m.opts.Context, m.opts.SessionAction
			return func() tea.Msg { return actionMsg{status: "Session stopped", err: run(ctx, "kill", id)} }, true
		}
		if r := m.boardSelected(); m.flowAttention && r != nil && r.PromptID != "" {
			if m.boardPromptFocus != r.PromptID || m.opts.Now().Sub(m.boardPromptAt) < 600*time.Millisecond {
				m.status = "Review prompt before answering"
				return nil, true
			}
			for _, p := range m.prompts {
				if p.ID == r.PromptID {
					return m.answerPrompt(p, k == "y"), true
				}
			}
		}
		m.status = "Jump to the terminal to answer this wait"
		return nil, true
	}
	return nil, false
}
func boardPad(s string, w int) string {
	s = truncate(clean(s), w)
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}
func boardMoney(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", *v)
}
func (m *app) armBoardPrompt() {
	if !m.flowAttention || len(m.board.Needs) == 0 {
		m.boardPromptFocus = ""
		return
	}
	r := m.board.Needs[min(m.flowAskCursor, len(m.board.Needs)-1)]
	if r.PromptID != m.boardPromptFocus {
		m.boardPromptFocus = r.PromptID
		m.boardPromptAt = m.opts.Now()
	}
}
func (m *app) boardLayout() (string, []flowHit) {
	m.syncBoard()
	b := m.board
	w := max(4, m.width)
	panelW := 0
	if w >= 120 {
		panelW = min(52, w/3)
		w -= panelW + 3
	}
	h := max(12, m.bodyHeight())
	lines := []string{}
	hits := []flowHit{}
	add := func(s string) { lines = append(lines, truncate(s, w)) }
	mode := m.flowViewMode
	if mode == "" {
		mode = "Map"
	}
	add("Flow  [m Map] [t Timeline] [s Sessions] · " + mode + " · last 60m")
	add(fmt.Sprintf("◆ %d needs you   ▶ %d   ‖ %d   ○ %d   ✓ %d", len(b.Needs), b.Pulse["working"], b.Pulse["stalled"], b.Pulse["idle"], b.Pulse["finished"]))
	add(view.BoardSpark(b.Spark) + " events/min · updated " + duration(max(time.Duration(0), m.opts.Now().Sub(b.ObservedAt))) + " ago")
	add(fmt.Sprintf("NEEDS YOU · %d · oldest first", len(b.Needs)))
	idx := 0
	if m.flowAttention {
		idx = min(m.flowAskCursor, max(0, len(b.Needs)-1))
	}
	if len(b.Needs) == 0 {
		add("  Nothing needs you")
	} else {
		r := b.Needs[idx]
		pre := "  "
		if m.flowAttention {
			pre = "› "
		}
		add(pre + "◆ " + r.Title + " · " + r.Node.Project + " · " + r.Age)
		command := r.Command
		if command == "" {
			command = r.WaitReason
		}
		// The complete command is available in a scrollable inspector on Enter;
		// the shelf wraps it without ellipsis, including at terminal widths of 80.
		for _, line := range strings.Split(wrapText(command, w-2), "\n") {
			add("  " + line)
		}
		action := "Enter open · J Terminal"
		if r.PromptID != "" {
			action = "y Allow · n Deny · Enter open full command"
		}
		add("  " + action)
		hits = append(hits, flowHit{y: 4, width: w, height: max(1, len(lines)-3), index: -idx - 1})
		shown := 1
		for i, r := range b.Needs {
			if i == idx || shown >= 3 {
				continue
			}
			add("  ◆ " + r.Title + " · " + r.Node.Project + " · " + r.Age)
			shown++
		}
		if len(b.Needs) > shown {
			add(fmt.Sprintf("  +%d more · Tab then j/k", len(b.Needs)-shown))
		}
	}
	add(strings.Repeat("─", w))
	rows := m.boardRows()
	m.boardCursor = max(0, min(m.boardCursor, len(rows)-1))
	type entry struct {
		text  string
		index int
	}
	entries := []entry{}
	titleW := min(38, max(20, w/3))
	trackW := max(4, w-titleW-25)
	render := func(r view.BoardRow, i int) string {
		pre := " "
		if i == m.boardCursor && !m.flowAttention {
			pre = "›"
		}
		title := boardPad(pre+" "+r.Glyph+" "+r.Title, max(5, titleW-r.Depth*3))
		if mode == "Sessions" {
			return title + " " + boardPad(r.Node.Adapter, 8) + " " + boardPad(r.Node.Project, max(8, w-titleW-20)) + " " + r.Age
		}
		overlap := ""
		if len(r.Overlaps) > 0 {
			overlap = " ┄ " + r.Overlaps[0]
		}
		return view.TokenStyle(r.State, m.opts.NoColor).Render(title) + " " + boardPad(r.Node.Adapter, 7) + " " + m.boardTrack(view.BoardStrip(r, trackW)) + " " + r.Age + overlap
	}
	if mode == "Sessions" {

		for i, r := range rows {
			entries = append(entries, entry{render(r, i), i})
		}
	} else {
		// Lip Gloss owns the tree connectors; the same preorder supplies hit targets.
		for _, g := range b.Groups {
			t := tree.New().Root(g.Name)
			branch := map[string]*tree.Tree{}
			indices := []int{}
			for i, r := range rows {
				if r.Node.Project != g.Name && !(r.Node.Project == "" && g.Name == "Unattached") {
					continue
				}
				n := tree.New().Root(render(r, i))
				branch[r.Key] = n
				if p := branch[r.Parent]; p != nil {
					p.Child(n)
				} else {
					t.Child(n)
				}
				indices = append(indices, i)
			}
			text := strings.Split(t.String(), "\n")
			for i, line := range text {
				index := -1
				if i > 0 && i-1 < len(indices) {
					index = indices[i-1]
				}
				entries = append(entries, entry{line, index})
			}
		}
	}
	selected := 0
	for i, e := range entries {
		if e.index == m.boardCursor {
			selected = i
			break
		}
	}
	reserved := 6
	if panelW > 0 {
		reserved = 0
	}
	room := max(1, h-len(lines)-reserved)
	start := max(0, selected-room+1)
	start = min(start, max(0, len(entries)-room))
	for i := start; i < min(len(entries), start+room); i++ {
		e := entries[i]
		add(e.text)
		if e.index >= 0 {
			hits = append(hits, flowHit{y: len(lines) - 1, width: w, height: 1, index: e.index})
		}
	}
	if len(entries) == 0 {
		add("  No sessions in this window")
	}
	detail := []string{}
	if r := m.boardSelected(); r != nil {
		detail = append(detail, r.Title+" · "+view.FlowJoin(r.Node.Project, r.Node.Adapter)+" · "+r.State)
		family := tree.New().Root("−60m                  now")
		familyNodes := map[string]*tree.Tree{}
		shown := 0
		total := 0
		for _, g := range b.Groups {
			for _, member := range g.Rows {
				if member.Root != r.Root {
					continue
				}
				total++
				if shown >= 5 {
					continue
				}
				shown++
				text := member.Glyph + " " + member.Title + "\n" + view.BoardStrip(member, max(8, panelW-10))
				node := tree.New().Root(text)
				familyNodes[member.Key] = node
				if parent := familyNodes[member.Parent]; parent != nil {
					parent.Child(node)
				} else {
					family.Child(node)
				}
			}
		}
		detail = append(detail, family.String())
		if total > shown {
			detail = append(detail, fmt.Sprintf("+%d more · open session", total-shown))
		}
		if r.Node.LastTool != "" {
			detail = append(detail, "now   "+r.Node.LastTool)
		}
		if len(r.Files) > 0 {
			names := []string{}
			for _, f := range r.Files {
				names = append(names, f.Path)
			}
			detail = append(detail, "files "+strings.Join(names, " · "))
			for _, name := range r.Overlaps {
				detail = append(detail, "┄ "+name+" · same file")
			}
		}
		if tools := view.BoardTools(*r); tools != "" {
			detail = append(detail, "tools "+tools+" · last hour")
		}
		if r.Cost != nil {
			detail = append(detail, "cost "+boardMoney(r.Cost)+" · retained window")
		}
	} else {
		detail = append(detail, "Select a session to see its family")
	}
	detail = append(detail, "J Terminal · Enter Open · c Ask Chat · K Stop")
	if panelW > 0 {
		left := strings.Join(lines, "\n")
		right := framedPanel("Selected family", "", wrapText(strings.Join(detail, "\n"), panelW-4), panelW, m.opts.NoColor)
		joined := strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(w).Render(left), " │ ", right), "\n")
		for i := range joined {
			joined[i] = strings.TrimRight(joined[i], " ")
		}
		return strings.Join(joined, "\n"), hits
	}
	add(strings.Repeat("─", w))
	if r := m.boardSelected(); r != nil {
		add(r.Title + " · " + r.Node.Adapter + " · " + r.State)
		if r.Node.LastTool != "" {
			add("now " + r.Node.LastTool)
		}
		if tools := view.BoardTools(*r); tools != "" {
			add(tools + " · last hour")
		}
		if r.Cost != nil {
			add("cost " + boardMoney(r.Cost) + " · retained window")
		}
	}
	add("Enter Open session · J Terminal · K Stop · c Ask Chat · Tab Needs you · / Find")

	return strings.Join(lines, "\n"), hits
}

func (m *app) boardTrack(track string) string {
	var out strings.Builder
	for _, ch := range track {
		token := "faint"
		switch ch {
		case '━':
			token = "working"
		case '◆':
			token = "waiting"
		case '─':
			token = "idle"
		case '✓', '●':
			token = "finished"
		case '┿':
			token = "accent"
		case '░':
			token = "faint"
		}
		out.WriteString(view.TokenStyle(token, m.opts.NoColor).Render(string(ch)))
	}
	return out.String()
}

func (m *app) selectFlowView(key string) tea.Cmd {
	m.flowViewMode = map[string]string{"m": "Map", "t": "Timeline", "s": "Sessions"}[key]
	if key == "s" {
		m.tab = 0
	} else {
		m.tab = flowTab
	}
	m.flowOffset = 0
	m.rebuild()
	return nil
}
