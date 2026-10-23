package tui

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/secfile"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

// Options configures scope, appearance, and injectable action dependencies.
type Options struct {
	Chat          chat.Client
	ChatEphemeral bool
	InitialTab    string
	MotionOff     bool
	Tmux          string
	JumpTerminal  bool
	Machine       string
	Project       string
	Theme         string
	NoColor       bool
	OpenCmd       string
	TermProgram   string
	Now           func() time.Time
	Exec          func([]string) error
	Kill          func(context.Context, model.Orphan, []int32, bool) (kill.Result, error)
	Spool         func(model.HookEvent) error
	Summarize     func(context.Context, string) error
	CleanupList   func(context.Context) ([]cleanup.Candidate, error)
	CleanupApply  func(context.Context, cleanup.Candidate) (cleanup.Result, error)
	DataDir       string
	Context       context.Context
	// LookupTmux resolves an ancestor pane PID to its tmux session.
	LookupTmux func(context.Context, []int32) (string, error)
}

type app struct {
	chatGeneration uint64
	chatActive     bool
	chatInput      textarea.Model
	chatContext    chat.Context
	chatID         string
	chatStatus     string
	chatMessages   []store.ChatMessage
	chatHistory    []store.Conversation
	chatDrafts     []string
	chatOffset     int
	chatEphemeral  bool
	chatTools      bool
	chatPolling    bool

	flow             view.FlowView
	flowID           int64
	flowMinutes      int
	flowAttention    bool
	flowAskCursor    int
	flowSelectID     int64
	flowOffset       int
	flowExpanded     bool
	flowPulse        map[string]time.Time
	flowBranchPulse  map[string]time.Time
	flowSeen         map[string]bool
	flowGrowthSeen   map[string]time.Time
	flowGrowthPulse  map[string]flowGrowthPulse
	flowAnimating    bool
	machineScope     string
	src              Source
	opts             Options
	snap             Snapshot
	loaded           bool
	pollPending      bool
	version          int64
	tab              int
	cursors          [9]int
	homeOffset       int
	homeNeedsTop     int
	cleanupRequested bool
	expanded         map[string]bool
	filter           textinput.Model
	filtering        bool
	sort             int
	group            bool
	table            table.Model
	rows             []tabRow
	detail           bool
	detailText       string
	detailRow        *tabRow
	viewport         viewport.Model
	confirm          *confirmation
	help             bool
	status           string
	busy             bool
	width, height    int
}
type confirmation struct {
	cleanup cleanup.Candidate
	kind    string
	orphan  model.Orphan
	force   bool
	items   []model.InboxItem
	ids     []string
}
type tickMsg struct{}
type heartbeatMsg struct{ err error }
type refreshMsg struct {
	snap    Snapshot
	version int64
	changed bool
	err     error
}
type actionMsg struct {
	status    string
	err       error
	cleanupID string
}
type cleanupMsg struct {
	candidates []cleanup.Candidate
	err        error
}
type cleanupSizeMsg struct {
	candidate cleanup.Candidate
	err       error
}

// New creates a Bubble Tea v2 model. Collection and actions run only in commands.
func New(src Source, opts Options) tea.Model {
	_ = view.SetTheme(opts.Theme)
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if opts.DataDir == "" {
		opts.DataDir = config.DataDir()
	}
	if opts.TermProgram == "" {
		opts.TermProgram = os.Getenv("TERM_PROGRAM")
	}
	if os.Getenv("NO_COLOR") != "" {
		opts.NoColor = true
	}
	if opts.Spool == nil {
		opts.Spool = func(e model.HookEvent) error { return spool.Append(config.SpoolDir(), e) }
	}
	if opts.Kill == nil {
		opts.Kill = func(ctx context.Context, o model.Orphan, p []int32, force bool) (kill.Result, error) {
			return kill.Tree(ctx, platform.New(), o, p, force, 5*time.Second)
		}
	}
	if opts.LookupTmux == nil {
		opts.LookupTmux = lookupTmux
	}
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "filter"
	input.SetStyles(textinput.Styles{})
	m := &app{chatInput: newChatInput(), chatEphemeral: opts.ChatEphemeral, tab: homeTab, machineScope: opts.Machine, src: src, opts: opts, width: 80, height: 24, expanded: map[string]bool{}, filter: input, table: table.New(table.WithFocused(true)), viewport: viewport.New()}
	if opts.InitialTab == "machine" {
		m.tab = 4
	}
	m.rebuild()
	return m
}

func (m *app) Init() tea.Cmd {
	m.pollPending = true
	return tea.Batch(m.poll(), nextTick(), tea.RequestBackgroundColor)
}
func nextTick() tea.Cmd { return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

// poll captures model values before starting IO, avoiding races with Update.
func (m *app) poll() tea.Cmd {
	src, opts, loaded, version := m.src, m.opts, m.loaded, m.version
	return func() tea.Msg {
		r := refreshMsg{version: version}
		if err := heartbeat(opts.DataDir, opts.Now()); err != nil {
			r.err = err
			return r
		}
		v, err := src.DataVersion(opts.Context)
		if err != nil {
			r.err = err
			return r
		}
		if loaded && v == version {
			return r
		}
		r.snap, r.err = src.Load(opts.Context)
		if r.err == nil {
			r.changed = true
			r.version = v
		}
		return r
	}
}

func heartbeat(dir string, now time.Time) error {
	if err := secfile.MkdirPrivate(dir); err != nil {
		return err
	}
	if err := secfile.Protect(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, "viewer.heartbeat")
	f, err := secfile.OpenFile(path, os.O_CREATE|os.O_WRONLY)
	if err != nil {
		return err
	}
	err = secfile.ProtectFile(f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chtimes(path, now, now)
}

func (m *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		view.SetDarkBackground(msg.IsDark())
		m.rebuild()
		return m, nil
	case chatStateMsg:
		if msg.generation != m.chatGeneration {
			if m.tab == chatTab {
				return m, m.readChat()
			}
			m.chatPolling = false
			return m, nil
		}
		m.chatActive = msg.state.Active
		if msg.err != nil {
			m.chatStatus = msg.err.Error()
			m.chatPolling = false
			return m, nil
		}
		m.chatID = msg.state.Conversation.ID
		m.chatMessages = msg.state.Messages
		m.chatHistory = msg.history
		m.chatEphemeral = msg.state.Ephemeral
		m.chatStatus = "Ready"
		if msg.state.Active {
			m.chatStatus = fmt.Sprintf("Claude is answering · %d queued", msg.state.Queued)
		}
		if m.tab == chatTab {
			return m, nextChatTick()
		}
		m.chatPolling = false
		return m, nil
	case chatSentMsg:
		if msg.err != nil {
			m.chatStatus = msg.err.Error()
			if msg.text != "" && m.chatInput.Value() == "" {
				m.chatInput.SetValue(msg.text)
			}
		} else if msg.text != "" {
			m.chatDrafts = append(m.chatDrafts, msg.text)
			if len(m.chatDrafts) > 100 {
				m.chatDrafts = m.chatDrafts[len(m.chatDrafts)-100:]
			}
		}
		return m, nil
	case chatTickMsg:
		if m.tab != chatTab {
			m.chatPolling = false
			return m, nil
		}
		return m, m.readChat()

	case flowObservationFrameMsg:
		for key, pulse := range m.flowGrowthPulse {
			if !m.opts.Now().Before(pulse.until) {
				delete(m.flowGrowthPulse, key)
			} else {
				pulse.frame++
				m.flowGrowthPulse[key] = pulse
			}
		}
		m.flowAnimating = false
		m.rebuild()
		return m, m.flowAnimation()
	case flowEventMsg:
		m.recordFlowEvent(msg.event)
	case flowMsg:
		if msg.id != m.flowID || msg.scope != m.machineScope {
			return m, nil
		}
		if msg.err != nil {
			m.status = "flow: " + msg.err.Error()
			return m, nil
		}
		var selectedID int64
		if i := m.cursors[flowTab]; i >= 0 && i < len(m.flowWorkers().Nodes) {
			selectedID = m.flowWorkers().Nodes[i].ID
		}
		if m.flowSelectID != 0 {
			selectedID = m.flowSelectID
			m.flowSelectID = 0
		}
		m.recordFlowObservations(msg.flow)
		if m.flow.Lead != nil && msg.flow.Lead != nil && m.flow.Lead.ID == msg.flow.Lead.ID {
			for _, n := range msg.flow.Children {
				if n.ParentID != nil && n.StartedAt.After(m.flow.ObservedAt) && !n.StartedAt.After(m.opts.Now()) {
					if m.flowBranchPulse == nil {
						m.flowBranchPulse = map[string]time.Time{}
					}
					m.flowBranchPulse[growthKey(n)] = m.opts.Now().Add(time.Second)
				}
			}
		}
		m.flow = msg.flow
		if m.flowSeen == nil {
			m.flowSeen = map[string]bool{}
		}
		for _, e := range m.flow.Tail {
			m.flowSeen[e.Key] = true
		}
		for i, n := range m.flowWorkers().Nodes {
			if n.ID == selectedID {
				m.cursors[flowTab] = i
			}
		}
		if m.snap.AgentDetails == nil {
			m.snap.AgentDetails = map[int64]view.AgentView{}
		}
		for id, d := range msg.details {
			m.snap.AgentDetails[id] = d
		}
		m.rebuild()
		return m, m.flowAnimation()
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		if m.tab == 0 && m.width >= 140 {
			m.detail = false
			m.detailRow = nil
		}
		m.rebuild()
		if m.detail && m.detailRow != nil {
			offset := m.viewport.YOffset()
			m.openDetail(*m.detailRow)
			m.viewport.SetYOffset(offset)
		}
	case tickMsg:
		if m.pollPending {
			opts := m.opts
			touch := func() tea.Msg { return heartbeatMsg{err: heartbeat(opts.DataDir, opts.Now())} }
			return m, tea.Batch(touch, nextTick())
		}
		m.pollPending = true
		return m, tea.Batch(m.poll(), nextTick())
	case heartbeatMsg:
		if msg.err != nil {
			m.status = "heartbeat: " + msg.err.Error()
		}
	case refreshMsg:
		m.pollPending = false
		if msg.err != nil {
			m.status = "refresh: " + msg.err.Error()
		}
		if msg.changed {
			if msg.snap.Cleanup == nil {
				msg.snap.Cleanup = m.snap.Cleanup
			}
			if m.loaded && m.tab == flowTab {
				seen := map[string]bool{}
				for _, e := range m.snap.Tail {
					seen[e.Key] = true
				}
				for _, e := range msg.snap.Tail {
					if !seen[e.Key] {
						m.recordFlowEvent(e)
					}
				}
			}
			m.snap = msg.snap
			m.version = msg.version
			m.loaded = true
		}
		if m.loaded {
			m.showAckPending()
			m.rebuild()
		}
		if msg.changed && m.tab == flowTab {
			return m, m.loadFlow()
		}
		if m.loaded && m.tab == homeTab && !m.cleanupRequested && m.snap.Cleanup == nil && m.opts.CleanupList != nil && !m.busy {
			m.cleanupRequested = true
			return m, m.loadCleanup()
		}
		return m, nil
	case cleanupMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "cleanup: " + msg.err.Error()
		} else {
			m.snap.Cleanup = msg.candidates
		}
		m.rebuild()
	case cleanupSizeMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "size: " + msg.err.Error()
			return m, nil
		}
		for i, c := range m.snap.Cleanup {
			if c.ID == msg.candidate.ID {
				m.snap.Cleanup[i] = msg.candidate
			}
		}
		if m.detail && m.detailRow != nil && m.detailRow.cleanup != nil && m.detailRow.cleanup.ID == msg.candidate.ID {
			r := *m.detailRow
			r.cleanup = &msg.candidate
			m.openDetail(r)
		}
		m.rebuild()
	case actionMsg:
		m.busy = false
		if msg.cleanupID != "" && msg.err == nil {
			cs := []cleanup.Candidate{}
			for _, c := range m.snap.Cleanup {
				if c.ID != msg.cleanupID {
					cs = append(cs, c)
				}
			}
			m.snap.Cleanup = cs
			m.detail = false
			m.detailRow = nil
		}
		m.status = msg.status
		if msg.err != nil {
			if m.status != "" {
				m.status += " · "
			}
			m.status += msg.err.Error()
		}
		m.rebuild()
	case tea.MouseClickMsg:
		return m, m.mouseClick(msg)
	case tea.MouseWheelMsg:
		return m, m.mouseWheel(msg)
	case tea.KeyPressMsg:
		k := msg.String()
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		if m.tab == chatTab {
			return m, m.chatKey(msg)
		}
		if (k == "c" || k == "8") && !m.filtering && m.confirm == nil {
			return m, m.openChat()
		}
		if m.filtering {
			switch k {
			case "esc":
				m.filter.SetValue("")
				m.filtering = false
				m.filter.Blur()
			case "enter":
				m.filtering = false
				m.filter.Blur()
			default:
				var cmd tea.Cmd
				m.filter, cmd = m.filter.Update(msg)
				m.rebuild()
				return m, cmd
			}
			m.rebuild()
			return m, nil
		}
		if k == "esc" && m.chatActive {
			return m, m.stopChat()
		}
		if k == "q" {
			return m, tea.Quit
		}
		if m.confirm != nil {
			return m, m.confirmKey(k)
		}
		if k == "esc" {
			m.detail = false
			m.help = false
			m.rebuild()
			return m, nil
		}
		if k == "?" {
			m.help = !m.help
			return m, nil
		}
		if m.help {
			return m, nil
		}
		if k >= "0" && k <= "7" {
			tab := int(k[0] - '1')
			if k == "0" {
				tab = homeTab
			}
			if k == "7" {
				tab = flowTab
			}
			if m.tabVisible(tab) {
				if tab == flowTab {
					// A tab key requests the shared default. Only explicit session
					// navigation pins an ID across refreshes.
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
					return m, m.loadFlow()
				}
				if tab == 5 && m.opts.CleanupList != nil && !m.busy {
					return m, m.loadCleanup()
				}
			}
			return m, nil
		}
		if m.tab == flowTab && !m.detail {
			if k == "tab" && len(m.flow.Attention.Waits) > 0 {
				m.flowAttention = !m.flowAttention
				return m, nil
			}
			if m.flowAttention && len(m.flow.Attention.Waits) > 0 {
				count := len(m.flow.Attention.Waits)
				m.flowAskCursor = min(m.flowAskCursor, count-1)
				w := m.flow.Attention.Waits[m.flowAskCursor]
				switch k {
				case "j", "down":
					m.flowAskCursor = (m.flowAskCursor + 1) % count
					return m, nil
				case "k", "up":
					m.flowAskCursor = (m.flowAskCursor + count - 1) % count
					return m, nil
				case "C":
					m.status = inbox.JumpCommand(w.Jump)
					return m, nil
				case "enter":
					return m, m.jumpFlowAttention(w)
				}
			}
			switch k {
			case "[", "]":
				delta := 1
				if k == "[" {
					delta = -1
				}
				m.flowSession(delta)
				return m, m.loadFlow()
			case "-", "+", "=":
				zooms := []int{15, 60, 240}
				i := 0
				for j, z := range zooms {
					if z == m.flowZoom() {
						i = j
					}
				}
				if k == "-" {
					i = min(2, i+1)
				} else {
					i = max(0, i-1)
				}
				m.flowMinutes = zooms[i]
				return m, m.loadFlow()
			case "C":
				if n := m.flowSelected(); n != nil {
					m.status = inbox.JumpCommand(n.Jump)
				}
				return m, nil
			case "enter":
				w := m.flowWorkers()
				if w.Quiet > 0 && m.cursors[flowTab] == len(w.Nodes) {
					m.flowExpanded = true
					m.rebuild()
					m.flowEnsureVisible()
					return m, nil
				}
				return m, m.jumpFlow()
			case "j", "down", "k", "up":
				w := m.flowWorkers()
				count := len(w.Nodes)
				if w.Quiet > 0 {
					count++
				}
				if count > 0 {
					delta := 1
					if k == "k" || k == "up" {
						delta = -1
					}
					m.cursors[flowTab] = (m.cursors[flowTab] + delta + count) % count
					m.rebuild()
					m.flowEnsureVisible()
				}
				return m, nil
			case "pgdown", "pgup":
				delta := m.bodyHeight()
				if k == "pgup" {
					delta = -delta
				}
				m.flowOffset = max(0, m.flowOffset+delta)
				return m, nil
			}
		}
		switch k {
		case "pgdown", "pgup":
			if m.tab == homeTab && !m.detail {
				delta := m.bodyHeight()
				if k == "pgup" {
					delta = -delta
				}
				m.homeOffset = max(0, m.homeOffset+delta)
				return m, nil
			}
			var cmd tea.Cmd
			if m.detail {
				m.viewport, cmd = m.viewport.Update(msg)
			} else {
				m.table, cmd = m.table.Update(msg)
				m.cursors[m.tab] = max(0, m.table.Cursor())
				m.rebuild()
			}
			return m, cmd
		case "r":
			if m.tab == 5 && m.opts.CleanupList != nil && !m.busy {
				m.detail = false
				return m, m.loadCleanup()
			}
		case "S":
			if m.tab == 1 && !m.busy {
				if row, ok := m.selected(); ok {
					if m.remote(row.machine) {
						m.status = "summarize is local-only"
						return m, nil
					}
					if m.opts.Summarize == nil {
						m.status = "run driga summarize for this project"
						return m, nil
					}
					run, ctx, path := m.opts.Summarize, m.opts.Context, row.cwd
					for _, project := range m.snap.Projects.Rows {
						if project.MachineID == row.machine && project.Status.Repo.ID == row.project {
							path = project.Status.Repo.Path
							break
						}
					}
					m.busy = true
					return m, func() tea.Msg {
						if err := run(ctx, path); err != nil {
							return actionMsg{err: err}
						}
						return actionMsg{status: "summary queued for collection"}
					}
				}
			}
		case "m":
			m.cycleMachine()
			if m.tab == flowTab {
				m.flowID = 0
				m.flow = view.FlowView{}
				m.flowExpanded = false
				m.flowOffset = 0
				m.flowPulse = nil
				m.flowSeen = nil
				m.rebuild()
				return m, m.loadFlow()
			}
			m.detail = false
			m.showAckPending()
			m.rebuild()
		case "/":
			m.filtering = true
			return m, m.filter.Focus()
		case "s":
			m.sort = (m.sort + 1) % 4
			m.status = "sort: " + []string{"state", "age", "mem", "project"}[m.sort]
			m.rebuild()
		case "g":
			if m.tab == 0 {
				m.group = !m.group
				m.rebuild()
			}
		case "right", "left":
			if r, ok := m.selected(); ok && !m.detail {
				m.expanded[r.key] = k == "right"
				m.rebuild()
			}
		case "enter":
			return m, m.openSelected(false)
		case "x", "space", " ":
			if m.tab == 5 && !m.busy {
				if r, ok := m.selected(); ok && r.cleanup != nil {
					if r.cleanup.Status != "ready" {
						m.status = "use driga cleanup " + r.cleanup.ID + " --include-ignored for needs-review; history is report-only"
					} else if m.opts.CleanupApply != nil {
						m.confirm = &confirmation{kind: "cleanup", cleanup: *r.cleanup}
					}
				}
				return m, nil
			}
			if (m.tab == 2 || m.tab == homeTab) && !m.busy {
				if r, ok := m.selected(); ok && r.inbox != nil {
					return m, m.ack([]model.InboxItem{*r.inbox})
				}
				if m.tab == homeTab {
					m.status = "no inbox event to acknowledge"
				}
			}
		case "X":
			if m.tab == 2 && !m.busy {
				var items []model.InboxItem
				for _, r := range m.rows {
					if r.inbox != nil {
						items = append(items, *r.inbox)
					}
				}
				if len(items) > 0 {
					m.confirm = &confirmation{kind: "ack-all", items: items}
					for _, item := range items {
						m.confirm.ids = append(m.confirm.ids, item.ID)
					}
				}
			}
		case "k", "K":
			if m.tab == 3 && !m.busy {
				if r, ok := m.selected(); ok && r.orphan != nil {
					o := *r.orphan
					if m.remote(r.machine) {
						m.status = fmt.Sprintf("orphan is on %s; run driga orphans kill there", m.machineName(r.machine))
					} else if o.Young {
						m.status = fmt.Sprintf("too young to kill (age %s)", duration(m.opts.Now().Sub(o.StartTime)))
					} else if !o.Verified {
						m.status = "unverified — run: driga orphans kill " + o.ID + " (checks it stays idle first)"
					} else {
						m.confirm = &confirmation{kind: "kill", orphan: o, force: k == "K"}
					}
				}
			}
		case "o":
			if !m.busy {
				if r, ok := m.selected(); ok && m.remote(r.machine) {
					m.status = "open a terminal on " + m.machineName(r.machine)
				} else if cwd := m.selectedCwd(); cwd != "" {
					args := openCommand(m.opts.OpenCmd, m.opts.TermProgram, cwd)
					return m, m.execute(args, false)
				}
			}
		case "a":
			if !m.busy {
				return m, m.attach()
			}
		default:
			if m.detail {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			m.cursors[m.tab] = max(0, m.table.Cursor())
			if m.tab == homeTab {
				m.homeOffset = 0
			}
			m.rebuild()
			return m, cmd
		}
	default:
		if m.tab == chatTab {
			var cmd tea.Cmd
			m.chatInput, cmd = m.chatInput.Update(msg)
			return m, cmd
		}
		if m.filtering {
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *app) confirmKey(k string) tea.Cmd {
	if k == "esc" || k == "n" || k == "N" {
		m.confirm = nil
		return nil
	}
	if k != "y" && k != "Y" {
		return nil
	}
	c := *m.confirm
	m.confirm = nil
	if c.kind == "ack-all" {
		return m.ack(c.items)
	}
	if c.kind == "cleanup" {
		run, ctx := m.opts.CleanupApply, m.opts.Context
		m.busy = true
		return func() tea.Msg {
			_, err := run(ctx, c.cleanup)
			return actionMsg{status: "cleanup " + c.cleanup.ID, err: err, cleanupID: c.cleanup.ID}
		}
	}
	if c.kind == "kill" && c.force {
		c.kind = "force"
		m.confirm = &c
		return nil
	}
	o := c.orphan
	if m.remote(o.MachineID) {
		m.status = fmt.Sprintf("orphan is on %s; run driga orphans kill there", m.machineName(o.MachineID))
		return nil
	}
	var expect []int32
	pids := o.PIDs
	if len(pids) == 0 {
		for _, p := range derive.NewProcIndex(m.snap.Procs).Descendants(o.RootPID) {
			pids = append(pids, p.PID)
		}
	}
	for _, pid := range pids {
		if pid != o.RootPID {
			expect = append(expect, pid)
		}
	}
	opts := m.opts
	m.busy = true
	return func() tea.Msg {
		command := "driga orphans kill " + o.ID
		if c.force {
			command += " --force"
		}
		r, err := opts.Kill(opts.Context, o, expect, c.force)
		status := fmt.Sprintf("%s · signaled %v; survivors %v", command, r.Signaled, r.Survivors)
		if r.Aborted {
			status += " · aborted: " + r.Reason
		}
		return actionMsg{status: status, err: err}
	}
}

func (m *app) ack(items []model.InboxItem) tea.Cmd {
	opts := m.opts
	var events []model.HookEvent
	var pending []string
	for _, item := range items {
		ev := model.HookEvent{Event: "ack", Message: item.ID, TS: opts.Now()}
		if m.remote(item.MachineID) {
			ev.MachineID = item.MachineID
			pending = append(pending, "ack pending → "+m.machineName(item.MachineID))
		}
		events = append(events, ev)
	}
	m.busy = true
	return func() tea.Msg {
		command := "driga inbox ack " + items[0].ID
		if len(items) > 1 {
			command = "driga inbox ack --all"
		}
		// Preflight the entire selection before writing any spool entries.
		for _, ev := range events {
			if ev.MachineID != "" {
				if err := fleet.CheckAckPermission(opts.DataDir, ev.MachineID); err != nil {
					return actionMsg{err: err}
				}
			}
		}
		for _, ev := range events {
			if err := opts.Spool(ev); err != nil {
				return actionMsg{status: command, err: err}
			}
		}
		if len(pending) > 0 {
			return actionMsg{status: strings.Join(pending, " · ")}
		}
		return actionMsg{status: command + " · ack recorded, applies on next collect"}
	}
}

func openCommand(template, term, cwd string) []string {
	if template == "" && runtime.GOOS == "windows" && pathx.WindowsStyle(cwd) {
		args, _ := inbox.WindowsTerminalCommand(cwd)
		return args
	}
	if template == "" {
		switch term {
		case "Apple_Terminal":
			template = "open -a Terminal {cwd}"
		case "iTerm.app":
			template = "open -a iTerm {cwd}"
		case "ghostty":
			template = "open -a Ghostty {cwd}"
		case "WezTerm":
			template = "wezterm start --cwd {cwd}"
		default:
			template = "open {cwd}"
		}
	}
	// Shield the replacement while splitting: spaces in cwd always stay in one arg.
	args := strings.Fields(strings.ReplaceAll(template, "{cwd}", "\x00"))
	for i := range args {
		args[i] = strings.ReplaceAll(args[i], "\x00", cwd)
	}
	return args
}

func (m *app) execute(args []string, attach bool) tea.Cmd {
	if len(args) == 0 {
		return nil
	}
	command := strings.Join(args, " ")
	m.status = command
	m.busy = true
	if m.opts.Exec != nil {
		run := m.opts.Exec
		return func() tea.Msg { return actionMsg{status: command, err: run(args)} }
	}
	ctx := m.opts.Context
	return func() tea.Msg {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		inbox.ConfigureCommand(cmd)
		if attach {
			return tea.ExecProcess(cmd, func(err error) tea.Msg { return actionMsg{status: command, err: err} })()
		}
		return actionMsg{status: command, err: cmd.Run()}
	}
}

func (m *app) showAckPending() {
	for _, row := range m.snap.Inbox.Rows {
		if row.AckPendingAt != nil && m.remote(row.MachineID) && m.machineVisible(row.MachineID) {
			m.status = "ack pending → " + m.machineName(row.MachineID)
			return
		}
	}
	if strings.HasPrefix(m.status, "ack pending → ") {
		m.status = ""
	}
}
