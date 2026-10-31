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

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

// Options configures scope, appearance, and injectable action dependencies.
type Options struct {
	SessionAction func(context.Context, string, int64) error
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
	// Signatures classifies orphan trees for the default Kill; nil means the
	// built-in set.
	Signatures   *signatures.Set
	Spool        func(model.HookEvent) error
	Summarize    func(context.Context, string) error
	CleanupList  func(context.Context) ([]cleanup.Candidate, error)
	CleanupApply func(context.Context, cleanup.Candidate) (cleanup.Result, error)
	DataDir      string
	Context      context.Context
	// LookupTmux resolves an ancestor pane PID to its tmux session.
	LookupTmux func(context.Context, []int32) (string, error)
}

type app struct {
	chatGeneration uint64
	chatActive     bool
	chatSpinner    spinner.Model
	chatAnimating  bool
	chatStarted    time.Time
	chatReplyID    string
	chatTool       string
	chatEventSeq   uint64
	chatInput      textarea.Model
	chatBodies     map[string]chatBodyRender
	chatContext    chat.Context
	chatID         string
	chatStatus     string
	chatMessages   []store.ChatMessage
	chatMode       chat.Mode
	chatUntil      time.Time // Full auto end, from the broker.
	chatFullAsk    bool      // m pressed once on the way to Full auto.
	grantHistory   []chat.GrantDecision
	bursts         []chat.PromptBurst
	burstFocus     string
	burstAt        time.Time
	burstIndex     int
	prompts        []chat.Prompt
	chatActions    []chat.Action
	chatAppeared   map[string]time.Time
	chatHistory    []store.Conversation
	chatDrafts     []string
	chatOffset     int
	chatEphemeral  bool
	chatTools      bool
	chatPolling    bool
	chatPageHeight int
	chatMaxOffset  int

	flowViewMode     string
	boardPromptFocus string
	boardPromptAt    time.Time
	chatOrigin       int
	chatOriginDetail bool
	boardPinned      string
	board            view.BoardView
	boardOrder       []string
	boardGroups      []string
	boardCursor      int
	boardTouched     bool  // the user moved the Flow selection
	boardStop        int64 // session awaiting Enter to confirm a stop
	boardMachine     bool
	boardFilter      int
	boardIdle        bool
	flowAttention    bool
	flowAskCursor    int
	flowOffset       int
	machineScope     string
	src              Source
	opts             Options
	snap             Snapshot
	loaded           bool
	pollPending      bool
	version          int64
	tab              int
	cursors          [9]int
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
	helpOffset       int
	selectionKey     string
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
	bursts       []chat.PromptBurst
	grantHistory []chat.GrantDecision
	prompts      []chat.Prompt
	snap         Snapshot
	version      int64
	changed      bool
	err          error
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
		sigs := opts.Signatures
		if sigs == nil {
			sigs = signatures.Builtin()
		}
		opts.Kill = func(ctx context.Context, o model.Orphan, p []int32, force bool) (kill.Result, error) {
			return kill.Tree(ctx, platform.New(), o, p, force, 5*time.Second, kill.WithClassifier(derive.NewClassifier(sigs, len(p)+1)))
		}
	}
	if opts.LookupTmux == nil {
		opts.LookupTmux = lookupTmux
	}
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "filter"
	input.SetStyles(textinput.Styles{})
	m := &app{chatInput: newChatInput(), chatSpinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)), chatEphemeral: opts.ChatEphemeral, tab: flowTab, machineScope: opts.Machine, src: src, opts: opts, width: 80, height: 24, expanded: map[string]bool{}, filter: input, table: table.New(table.WithFocused(true)), viewport: viewport.New()}
	switch opts.InitialTab {
	case "projects":
		m.tab = 1
	case "machines":
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
		if client, ok := opts.Chat.(chat.PromptClient); ok {
			ctx, cancel := context.WithTimeout(opts.Context, time.Second)
			r.prompts, _ = client.Prompts(ctx)
			cancel()
		}
		if client, ok := opts.Chat.(chat.BurstClient); ok {
			ctx, cancel := context.WithTimeout(opts.Context, time.Second)
			r.bursts, _ = client.PromptBursts(ctx)
			cancel()
		}
		if client, ok := opts.Chat.(chat.GrantClient); ok {
			ctx, cancel := context.WithTimeout(opts.Context, time.Second)
			r.grantHistory, _ = client.GrantHistory(ctx)
			cancel()
		}
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
	model, cmd := m.update(msg)
	return model, tea.Batch(cmd, m.loadSelection())
}
func (m *app) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case selectedDetailMsg:
		if msg.key != m.selectionKey {
			return m, nil
		}
		if msg.err != nil {
			m.status = "detail: " + msg.err.Error()
			return m, nil
		}
		if msg.project != nil {
			if m.snap.ProjectDetails == nil {
				m.snap.ProjectDetails = map[int64]view.WhereView{}
			}
			m.snap.ProjectDetails[msg.id] = *msg.project
		} else {
			m.snap.AgentDetails = map[int64]view.AgentView{msg.id: msg.detail}
		}
		m.rebuild()
		if m.detail && m.detailRow != nil && (msg.project == nil && m.detailRow.agent == msg.id || msg.project != nil && m.detailRow.project == msg.id) {
			offset := m.viewport.YOffset()
			m.openDetail(*m.detailRow)
			m.viewport.SetYOffset(offset)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		view.SetDarkBackground(msg.IsDark())
		m.rebuild()
		return m, nil
	case chatStateMsg:
		if msg.generation != m.chatGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.endChatActivity()
			m.chatStatus = msg.err.Error()
			m.chatPolling = false
			return m, nil
		}
		oldOffset, oldMax := m.chatOffset, m.chatMaxOffset
		if oldOffset > 0 {
			m.chatView()
			oldOffset, oldMax = m.chatOffset, m.chatMaxOffset
		}
		m.chatID = msg.state.Conversation.ID
		m.chatMessages = msg.state.Messages
		if msg.state.Reset {
			m.chatActions = msg.state.Actions
		} else {
			for _, event := range msg.state.Events {
				if event.Action == nil || event.Seq <= m.chatEventSeq {
					continue
				}
				found := false
				for i := range m.chatActions {
					if m.chatActions[i].ID == event.Action.ID {
						m.chatActions[i], found = *event.Action, true
						break
					}
				}
				if !found {
					m.chatActions = append(m.chatActions, *event.Action)
				}
			}
			if len(m.chatActions) > chat.MaxMessages {
				m.chatActions = m.chatActions[len(m.chatActions)-chat.MaxMessages:]
			}
		}
		bell := m.noticeChatActions()
		m.chatMode, m.chatUntil = msg.state.Mode, msg.state.Until
		m.chatHistory = msg.history
		m.chatEphemeral = msg.state.Ephemeral
		m.updateChatActivity(msg.state)
		m.chatEventSeq = max(m.chatEventSeq, msg.state.Cursor)
		if oldOffset > 0 {
			m.chatView()
			m.chatOffset = min(m.chatMaxOffset, max(0, oldOffset+m.chatMaxOffset-oldMax))
		}
		if m.chatActive {
			return m, tea.Batch(bell, nextChatTick(m.chatGeneration), m.animateChat())
		}
		m.chatPolling = false
		return m, bell
	case chatSentMsg:
		if msg.generation != m.chatGeneration {
			return m, nil
		}
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
		if msg.err == nil && m.opts.Chat != nil && !m.chatPolling {
			m.chatPolling = true
			return m, m.readChat()
		}
		return m, nil
	case spinner.TickMsg:
		if !m.chatActive || m.pendingChat() != nil || m.tab != chatTab || m.opts.NoColor || m.opts.MotionOff {
			m.chatAnimating = false
			return m, nil
		}
		var cmd tea.Cmd
		m.chatSpinner, cmd = m.chatSpinner.Update(msg)
		return m, cmd
	case chatTickMsg:
		if msg.generation != m.chatGeneration {
			return m, nil
		}
		if !m.chatActive || m.opts.Chat == nil {
			m.chatPolling = false
			return m, nil
		}
		return m, m.readChat()

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
		if m.detailRow != nil && m.detailRow.prompt != nil {
			found := false
			for _, p := range msg.prompts {
				if p.ID == m.detailRow.prompt.ID {
					found = true
				}
			}
			if !found {
				m.detail = false
				m.detailRow = nil
				m.detailText = ""
				m.viewport.SetContent("")
			}
		}
		m.bursts = msg.bursts
		if m.detailRow != nil && m.detailRow.burst != nil {
			old := m.detailRow.burst
			next := m.promptBurst(old.ID)
			if next == nil {
				m.detail = false
				m.detailRow = nil
				m.detailText = ""
				m.viewport.SetContent("")
			} else if burstIdentity(*next) != burstIdentity(*old) {
				r := *m.detailRow
				r.burst = next
				m.openDetail(r)
			}
		}
		m.prompts = msg.prompts
		m.grantHistory = msg.grantHistory
		m.pollPending = false
		if msg.err != nil {
			m.status = "refresh: " + msg.err.Error()
		}
		if msg.changed {
			if msg.snap.Cleanup == nil {
				msg.snap.Cleanup = m.snap.Cleanup
			}
			m.snap = msg.snap
			m.version = msg.version
			m.loaded = true
		}
		if m.loaded {
			m.showAckPending()
			m.rebuild()
			if m.flowAttention {
				m.armBoardPrompt()
			}
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
		// Modified tab keys bypass focused composers, filters and overlays.
		if msg.Mod == tea.ModAlt && msg.Code >= '0' && msg.Code <= '2' {
			return m, m.selectTab(byte(msg.Code))
		}
		if m.chatFullAsk && k != "m" {
			m.chatFullAsk = false
			m.chatStatus = "Full auto not enabled."
		}
		if m.tab == chatTab && k == "esc" && !m.help && !m.chatActive {
			m.tab = m.chatOrigin
			m.detail = m.chatOriginDetail
			m.chatInput.Blur()
			m.rebuild()
			return m, nil
		}
		if m.tab == chatTab && !m.filtering && !m.help {
			if k == "esc" || m.chatInput.Focused() {
				return m, m.chatKey(msg)
			}
			if k == "c" || k == "enter" {
				return m, m.chatInput.Focus()
			}
			switch k {
			case "ctrl+n", "ctrl+h", "ctrl+x", "ctrl+t", "ctrl+0", "pgup", "pgdown", "ctrl+end", "alt+up", "y", "n", "alt+y", "alt+n", "m", "r":
				return m, m.chatKey(msg)
			}
		}
		if k == "c" && !m.filtering && m.confirm == nil {
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
			if !m.detail && !m.help {
				m.filter.SetValue("")
			}
			m.detail = false
			m.help = false
			m.rebuild()
			return m, nil
		}
		if k == "?" {
			m.help = !m.help
			m.helpOffset = 0
			return m, nil
		}
		if m.help {
			switch k {
			case "j", "down":
				m.scrollHelp(1)
			case "k", "up":
				m.scrollHelp(-1)
			case "pgdown":
				m.scrollHelp(max(1, m.bodyHeight()-1))
			case "pgup":
				m.scrollHelp(-max(1, m.bodyHeight()-1))
			}
			return m, nil
		}
		if k >= "0" && k <= "2" {
			return m, m.selectTab(k[0])
		}
		if !m.detail {
			if (m.tab == flowTab || m.tab == 0 || m.tab == 2) && k == "h" {
				if m.tab == 2 {
					m.tab = flowTab
				} else {
					m.tab = 2
				}
				m.rebuild()
				return m, nil
			}
			if m.tab == 0 && (k == "m" || k == "t") {
				m.tab = flowTab
				return m, m.selectFlowView(k)
			}
			if m.tab == 4 || m.tab == 3 || m.tab == 5 {
				switch k {
				case "p":
					m.tab = 3
					m.rebuild()
					return m, nil
				case "d":
					m.tab = 5
					m.rebuild()
					if m.opts.CleanupList != nil {
						return m, m.loadCleanup()
					}
					return m, nil
				case "h":
					m.tab = 4
					m.rebuild()
					return m, nil
				}
			}
		}
		if m.detail && m.detailRow != nil && m.detailRow.burst != nil {
			if cmd, handled := m.burstKey(k); handled {
				return m, cmd
			}
		}
		if m.tab == flowTab && !m.detail {
			if cmd, handled := m.boardKey(k); handled {
				return m, cmd
			}
		}
		switch k {
		case "pgdown", "pgup":
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
			if m.tab == 2 && !m.busy {
				if row, ok := m.selected(); ok && row.grant != nil {
					client, ok := m.opts.Chat.(chat.GrantClient)
					if ok {
						id, ctx := row.grant.GrantID, m.opts.Context
						return m, func() tea.Msg { return actionMsg{status: "Grant revoked", err: client.RevokeGrant(ctx, id)} }
					}
				}
			}
			if m.tab == 5 && m.opts.CleanupList != nil && !m.busy {
				m.detail = false
				return m, m.loadCleanup()
			}
		case "S":
			if m.tab == 0 {
				m.sort = (m.sort + 1) % 4
				m.rebuild()
				return m, nil
			}
			if m.tab == 1 && !m.busy {
				if row, ok := m.selected(); ok {
					if m.remote(row.machine) {
						m.status = "summarize is local-only"
						return m, nil
					}
					if m.opts.Summarize == nil {
						m.status = "run driga projects summarize <project>"
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
		case "M":
			m.cycleMachine()
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
						m.status = "use driga cleanup remove " + r.cleanup.ID + " --include-ignored for needs-review; history is report-only"
					} else if m.opts.CleanupApply != nil {
						m.confirm = &confirmation{kind: "cleanup", cleanup: *r.cleanup}
					}
				}
				return m, nil
			}
			if (m.tab == 2) && !m.busy {
				if r, ok := m.selected(); ok && r.inbox != nil {
					return m, m.ack([]model.InboxItem{*r.inbox})
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
		case "y", "n":
			if m.tab == 2 && !m.busy {
				if r, ok := m.selected(); ok && r.burst != nil {
					m.openDetail(r)
					return m, nil
				}
				if r, ok := m.selected(); ok && r.prompt != nil {
					return m, m.answerPrompt(*r.prompt, k == "y")
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
		args, _ := attention.WindowsTerminalCommand(cwd)
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
		attention.ConfigureCommand(cmd)
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

type selectedDetailMsg struct {
	key     string
	id      int64
	detail  view.AgentView
	project *view.WhereView
	err     error
}

func (m *app) loadSelection() tea.Cmd {
	source, ok := m.src.(viewer.DetailSource)
	if !ok || !m.loaded || m.tab == chatTab || m.tab == flowTab && !m.detail {
		return nil
	}
	row, ok := m.selected()
	if m.detail && m.detailRow != nil {
		row, ok = *m.detailRow, true
	}
	if !ok || row.agent == 0 && row.project == 0 {
		m.selectionKey = ""
		return nil
	}
	key := fmt.Sprintf("%d/%s/%d/%d", m.version, row.machine, row.agent, row.project)
	if key == m.selectionKey {
		return nil
	}
	m.selectionKey = key
	ctx, scope, id, project := m.opts.Context, m.flowScope(), row.agent, row.project
	if row.machine != "" {
		scope = store.Scope{Machines: []string{row.machine}}
	}
	return func() tea.Msg {
		if project != 0 && id == 0 {
			detail, err := source.LoadProject(ctx, scope, project, false)
			return selectedDetailMsg{key: key, id: project, project: &detail, err: err}
		}
		detail, err := source.LoadAgent(ctx, scope, id)
		return selectedDetailMsg{key: key, id: id, detail: detail, err: err}
	}
}
