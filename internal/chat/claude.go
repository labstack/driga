package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/self"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/llm"
)

var ReadTools = []string{"get_flow", "get_activity", "get_summary", "list_agents", "get_agent", "list_projects", "inspect_project", "list_inbox", "list_conflicts", "list_orphans", "get_machine", "list_machines", "inspect_setup", "list_cleanup_candidates", "describe_panels", "show_panels", "open_dashboard", "list_dashboards", "list_dashboard_versions"}

// NativeTools are the claude built-in tools Chat may use when it can act:
// reading files and its own task list. It never runs commands or changes
// files; what Chat changes goes through Driga's own tools and their cards.
var NativeTools = []string{"Read", "Glob", "Grep", "TodoWrite"}

// DashboardWrites change only the user's dashboards, each save a version that
// can be restored, so Chat runs them without an approval card.
var DashboardWrites = []string{"create_dashboard", "edit_dashboard", "replace_dashboard", "restore_dashboard_version"}

func readTool(name string) bool {
	for _, t := range ReadTools {
		if name == "mcp__driga__"+t {
			return true
		}
	}
	return false
}

type Claude struct {
	probeMu             sync.Mutex
	probed              bool
	cli                 llm.Claude
	err                 error
	DataDir, Executable string
}

func (a *Claude) probe(ctx context.Context) (llm.Claude, error) {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	if a.probed {
		return a.cli, a.err
	}
	c, err := llm.Probe(ctx)
	if err == nil {
		err = c.Require("print", "setting-sources", "strict-mcp-config", "mcp-config", "tools", "allowedTools", "name", "output-format", "verbose", "include-partial-messages", "resume")
	}
	// Stop during startup is transient; the next turn must be able to retry.
	if ctx.Err() == nil {
		a.cli, a.err, a.probed = c, err, true
	}
	return c, err
}
func (a *Claude) Run(ctx context.Context, r Request, emit func(Delta) error) error {
	c, err := a.probe(ctx)
	if err != nil {
		return err
	}
	actions := !r.ReadOnly && r.Socket != "" && r.Token != "" && c.Require("permission-prompts") == nil
	if !r.ReadOnly && r.Socket != "" && !actions && c.Require("permission-prompts") != nil {
		if err := emit(Delta{Notice: "Chat stays read-only: " + c.Require("permission-prompts").Error()}); err != nil {
			return err
		}
	}
	dir := filepath.Join(a.DataDir, "tmp")
	if err := secfile.MkdirPrivate(dir); err != nil {
		return err
	}
	f, err := secfile.CreateTemp(dir, "chat-mcp-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	exe := a.Executable
	if exe == "" {
		exe, err = self.Executable()
		if err != nil {
			f.Close()
			return err
		}
	}
	// Paths are explicit so an MCP child launched from a selected project still
	// reads the same data/config as its broker, independent of its cwd.
	env, err := mcpEnvironment(a.DataDir)
	if err != nil {
		f.Close()
		return err
	}
	if actions {
		env["DRIGA_CHAT_SOCKET"], env["DRIGA_CHAT_CONVERSATION"], env["DRIGA_CHAT_TOKEN"] = r.Socket, r.Conversation, r.Token
		env["DRIGA_CHAT_READ_ONLY"] = "0"
	} else {
		env["DRIGA_CHAT_READ_ONLY"] = "1"
	}
	conf := map[string]any{"mcpServers": map[string]any{"driga": map[string]any{"command": exe, "args": []string{"mcp", "--chat"}, "env": env}}}
	encodeErr := json.NewEncoder(f).Encode(conf)
	if err := errors.Join(encodeErr, f.Close()); err != nil {
		return err
	}
	tools := make([]string, len(ReadTools))
	for i, t := range ReadTools {
		tools[i] = "mcp__driga__" + t
	}
	nativeTools := ""
	if actions {
		nativeTools = strings.Join(NativeTools, ",")
		// Claude permits reads in cwd itself; pre-allowing read tools would
		// also bypass the permission bridge for paths outside that directory.
		tools = append(tools, "TodoWrite", "mcp__driga__list_prompts")
		for _, t := range DashboardWrites {
			tools = append(tools, "mcp__driga__"+t)
		}
	}
	// --safe-mode also drops --mcp-config servers; empty setting sources keep
	// user hooks, plugins and MCP out while the explicit Driga service loads.
	args := []string{"-p", "--setting-sources", "", "--strict-mcp-config", "--mcp-config", f.Name(), "--tools", nativeTools, "--allowedTools", strings.Join(tools, ","), "--name", "driga-internal", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if actions {
		args = append(args, "--permission-prompts", "host", "--permission-prompt-tool", "mcp__driga__approve")
	}
	if r.Resume != "" {
		args = append(args, "--resume", r.Resume)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := stream{emit: emit, cancel: cancel, actions: actions}
	run := c.RunIn
	if actions {
		run = c.RunChatIn
	}
	runErr := run(ctx, r.Directory, args, r.Prompt, &s)
	if s.err != nil {
		return s.err
	}
	if runErr != nil {
		return runErr
	}
	return s.finish()
}

func mcpEnvironment(dataDir string) (map[string]string, error) {
	dataPath, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	spoolPath, err := filepath.Abs(config.SpoolDir())
	if err != nil {
		return nil, err
	}
	env := map[string]string{"DRIGA_DATA_DIR": dataPath, "DRIGA_SPOOL_DIR": spoolPath}
	// Preserve the distinction between an optional default config and an
	// explicit override when Claude starts the MCP child from a project cwd.
	if os.Getenv("DRIGA_CONFIG") != "" {
		env["DRIGA_CONFIG"], err = filepath.Abs(config.Path())
		if err != nil {
			return nil, err
		}
	}
	return env, nil
}

// stream is the bounded JSONL parser from ask, extended for partial text and
// vendor session IDs. It never treats tool inputs/results as assistant text.
type stream struct {
	emit                              func(Delta) error
	cancel                            context.CancelFunc
	pending                           []byte
	partialText                       string
	tools                             map[string]bool
	actions                           bool
	connected, partial, printed, done bool
	// gap is set when a new model message starts after text was shown, so
	// its text begins a new paragraph rather than running on.
	gap bool
	// cards are panel tool calls waiting for their result, by tool use id.
	cards map[string]card
	// offered is the tools claude's init event listed, all of them ones
	// Chat accepts; nil until it arrives.
	offered map[string]bool
	err     error
}

// text emits reply text, starting a new paragraph after an earlier message.
func (s *stream) text(t string) error {
	if s.gap {
		t, s.gap = "\n\n"+t, false
	}
	s.printed = true
	return s.emit(Delta{Text: t})
}

const maxLine = 1 << 20

func (s *stream) Write(p []byte) (int, error) {
	n := len(p)
	if s.err != nil {
		return 0, s.err
	}
	for len(p) > 0 {
		line, rest, found := bytes.Cut(p, []byte{'\n'})
		if len(s.pending)+len(line) > maxLine {
			return 0, s.fail(errors.New("bad claude output: stream line exceeds limit"))
		}
		s.pending = append(s.pending, line...)
		if !found {
			break
		}
		if err := s.line(s.pending); err != nil {
			return 0, s.fail(err)
		}
		s.pending = s.pending[:0]
		p = rest
	}
	return n, nil
}
func (s *stream) fail(err error) error { s.err = err; s.cancel(); return err }
func (s *stream) line(line []byte) error {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	var e struct {
		Type, Subtype string
		SessionID     string                          `json:"session_id"`
		MCPServers    []struct{ Name, Status string } `json:"mcp_servers"`
		Tools         []string                        `json:"tools"`
		Message       struct {
			Content []struct {
				Type, Text, Name, ID string
				Input                json.RawMessage `json:"input"`
				ToolUseID            string          `json:"tool_use_id"`
				Content              json.RawMessage `json:"content"`
				IsError              bool            `json:"is_error"`
			}
		} `json:"message"`
		Event struct {
			Type         string
			ContentBlock struct{ Type, Name, ID string } `json:"content_block"`
			Delta        struct{ Type, Text string }
		} `json:"event"`
		Result  *string `json:"result"`
		IsError bool    `json:"is_error"`
	}
	if err := json.Unmarshal(line, &e); err != nil || e.Type == "" {
		return errors.New("bad claude output: expected stream-json object")
	}
	if s.done {
		return errors.New("bad claude output: event after final result")
	}
	if e.Type == "system" && e.Subtype == "init" {
		for _, server := range e.MCPServers {
			if server.Name == "driga" && server.Status == "connected" {
				s.connected = true
			} else {
				return errors.New("unexpected or unavailable driga MCP server")
			}
		}
		if !s.connected {
			return errors.New("driga MCP server did not connect; refusing Chat")
		}
		s.offered = map[string]bool{}
		for _, tool := range e.Tools {
			if !s.validTool(tool) {
				return fmt.Errorf("unexpected tool %q; Chat requires driga read tools only", tool)
			}
			s.offered[tool] = true
		}
	}
	switch e.Type {
	case "system":
	case "assistant", "stream_event", "result", "user":
		if !s.connected {
			return errors.New("driga MCP server connection was not confirmed")
		}
	default:
		return nil // Forward compatible metadata; never rendered as text.
	}
	if e.SessionID != "" && (len(e.SessionID) > 128 || strings.HasPrefix(e.SessionID, "-") || strings.ContainsAny(e.SessionID, " /\\\t\r\n")) {
		return errors.New("bad claude output: invalid session ID")
	}
	if e.SessionID != "" {
		if err := s.emit(Delta{SessionID: e.SessionID}); err != nil {
			return err
		}
	}
	switch e.Type {
	case "stream_event":
		if e.Event.Type == "message_start" {
			s.partial = false
			s.partialText = ""
			s.gap = s.printed
		}
		if e.Event.Type == "content_block_start" && e.Event.ContentBlock.Type == "tool_use" {
			return s.tool(e.Event.ContentBlock.ID, e.Event.ContentBlock.Name)
		}
		if e.Event.Type == "content_block_delta" && e.Event.Delta.Type == "text_delta" {
			s.partial = true
			s.partialText += e.Event.Delta.Text
			return s.text(e.Event.Delta.Text)
		}
	case "user":
		for _, content := range e.Message.Content {
			if content.Type == "tool_result" {
				if err := s.emit(Delta{ToolUseID: content.ToolUseID, ToolResult: string(content.Content), Failed: content.IsError}); err != nil {
					return err
				}
				if err := s.settle(content.ToolUseID, content.Content, content.IsError); err != nil {
					return err
				}
			}
		}

	case "assistant":
		var text strings.Builder
		for _, content := range e.Message.Content {
			if content.Type == "text" {
				text.WriteString(content.Text)
			}
			if content.Type == "tool_use" {
				if err := s.tool(content.ID, content.Name); err != nil {
					return err
				}
				if err := s.panel(content.ID, content.Name, content.Input); err != nil {
					return err
				}
			}
		}
		full := text.String()
		if s.partial {
			if strings.HasPrefix(full, s.partialText) {
				full = strings.TrimPrefix(full, s.partialText)
			} else {
				full = "" // Never duplicate text already delivered by partial events.
			}
		}
		if full != "" {
			if err := s.text(full); err != nil {
				return err
			}
		}
		s.partial = false
		s.partialText = ""
	case "result":
		if e.IsError {
			return errors.New("bad claude output: final result reports an error")
		}
		if e.Result == nil {
			return errors.New("bad claude output: missing result text")
		}
		s.done = true
		// A card whose result never came still shows what was asked.
		for id := range s.cards {
			if err := s.settle(id, nil, false); err != nil {
				return err
			}
		}
		if !s.printed {
			if err := s.emit(Delta{Text: *e.Result}); err != nil {
				return err
			}
		}
		return s.emit(Delta{Result: append(json.RawMessage{}, line...)})
	}
	return nil
}
func (s *stream) finish() error {
	if len(bytes.TrimSpace(s.pending)) > 0 {
		if err := s.line(s.pending); err != nil {
			return err
		}
	}
	if !s.done {
		return errors.New("bad claude output: missing final result")
	}
	return nil
}

func (s *stream) tool(id, name string) error {
	if !s.validTool(name) {
		// A model resuming a conversation may still ask for a tool it once
		// had. claude refuses a tool its init did not offer, and the model
		// goes on; only a refused tool that was offered ends the turn.
		if s.offered != nil && !s.offered[name] {
			return nil
		}
		return errors.New("agent attempted a tool outside driga read tools")
	}
	if s.tools == nil {
		s.tools = map[string]bool{}
	}
	if id != "" && s.tools[id] {
		return nil
	}
	if len(s.tools) >= 256 {
		return errors.New("too many tool calls in Chat turn")
	}
	if id != "" {
		s.tools[id] = true
	}
	return s.emit(Delta{Tool: name})
}

// panelTools render as a live card in the app's conversation.
var panelTools = []string{"show_panels", "open_dashboard", "create_dashboard", "edit_dashboard", "replace_dashboard", "restore_dashboard_version"}

// maxPanelInput bounds a card's spec; a larger one stays a tool line.
const maxPanelInput = 128 << 10

// card is what the app shows for a panel tool call: the tool and its input,
// which the app runs again for live data, and what came of it. Saved names
// the dashboard a write saved; Error is why the call failed, such as a
// rejected draft.
type card struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	Saved *struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	} `json:"saved,omitempty"`
	Error string `json:"error,omitempty"`
}

// panel holds a panel tool call until its result says what came of it.
func (s *stream) panel(id, name string, input json.RawMessage) error {
	tool, ok := strings.CutPrefix(name, "mcp__driga__")
	if !ok || !slices.Contains(panelTools, tool) || len(input) == 0 || len(input) > maxPanelInput || !json.Valid(input) {
		return nil
	}
	if s.cards == nil {
		s.cards = map[string]card{}
	}
	s.cards[id] = card{Tool: tool, Input: input}
	return nil
}

// settle emits a held card with its result.
func (s *stream) settle(id string, result json.RawMessage, failed bool) error {
	c, ok := s.cards[id]
	if !ok {
		return nil
	}
	delete(s.cards, id)
	text := resultText(result)
	if failed {
		c.Error = scrub.Snippet(text)
	} else if c.Tool != "show_panels" && c.Tool != "open_dashboard" {
		// claude passes the tool's structured result, the saved dashboard's
		// fragment, as the result text.
		var f struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		}
		if json.Unmarshal([]byte(text), &f) == nil && f.ID != "" {
			c.Saved = &struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			}{f.ID, f.Version}
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.emit(Delta{Panel: b})
}

// resultText is a tool result's text: claude sends it as a string or as
// content blocks.
func resultText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct{ Type, Text string }
	_ = json.Unmarshal(raw, &blocks)
	var b strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

func (s *stream) validTool(name string) bool {
	if readTool(name) {
		return true
	}
	if !s.actions {
		return false
	}
	if !strings.HasPrefix(name, "mcp__") {
		// ToolSearch is how claude loads deferred tool definitions.
		return name == "ToolSearch" || slices.Contains(NativeTools, name)
	}
	switch name {
	case "mcp__driga__create_dashboard", "mcp__driga__edit_dashboard", "mcp__driga__replace_dashboard", "mcp__driga__restore_dashboard_version", "mcp__driga__delete_dashboard":
		return true
	case "mcp__driga__list_prompts", "mcp__driga__answer_prompt", "mcp__driga__ack_inbox", "mcp__driga__jump", "mcp__driga__kill", "mcp__driga__cleanup", "mcp__driga__summarize", "mcp__driga__approve":
		return true
	}
	return false
}

// PanelTitle names a panel card for surfaces that cannot render it.
func PanelTitle(card string) string {
	var c struct {
		Tool  string `json:"tool"`
		Input struct {
			ID        string `json:"id"`
			Dashboard struct {
				Name string `json:"name"`
			} `json:"dashboard"`
		} `json:"input"`
	}
	if json.Unmarshal([]byte(card), &c) != nil {
		return "panel"
	}
	switch {
	case c.Input.Dashboard.Name != "":
		return c.Input.Dashboard.Name
	case c.Input.ID != "":
		return "dashboard " + c.Input.ID
	}
	return "panel"
}
