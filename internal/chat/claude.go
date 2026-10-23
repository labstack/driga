package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/secfile"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/llm"
)

// Keep read authority explicit, including when the permission bridge is unavailable.
var ReadBuiltins = []string{"Read", "Glob", "Grep", "LS", "TodoWrite", "NotebookRead"}

var ReadTools = []string{"get_flow", "get_activity", "get_summary", "list_agents", "get_agent", "list_projects", "inspect_project", "list_inbox", "list_conflicts", "list_orphans", "get_machine", "list_machines", "inspect_setup", "list_cleanup_candidates", "list_prompts"}

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
	warned              bool
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
		a.probeMu.Lock()
		warn := !a.warned
		a.warned = true
		a.probeMu.Unlock()
		if warn {
			if err := emit(Delta{Notice: "Chat stays read-only: " + c.Require("permission-prompts").Error()}); err != nil {
				return err
			}
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
		exe, err = os.Executable()
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
		nativeTools = "default"
		tools = append(tools, ReadBuiltins...)
	}
	// --safe-mode also drops --mcp-config servers; empty setting sources keep
	// user hooks, plugins and MCP out while the explicit driga server loads.
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
	err                               error
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
		for _, tool := range e.Tools {
			if !s.validTool(tool) {
				return fmt.Errorf("unexpected tool %q; Chat requires driga read tools only", tool)
			}
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
		}
		if e.Event.Type == "content_block_start" && e.Event.ContentBlock.Type == "tool_use" {
			if !s.validTool(e.Event.ContentBlock.Name) {
				return errors.New("agent attempted a tool outside driga read tools")
			}
			return s.tool(e.Event.ContentBlock.ID, e.Event.ContentBlock.Name)
		}
		if e.Event.Type == "content_block_delta" && e.Event.Delta.Type == "text_delta" {
			s.partial = true
			s.partialText += e.Event.Delta.Text
			s.printed = true
			return s.emit(Delta{Text: e.Event.Delta.Text})
		}
	case "user":
		for _, content := range e.Message.Content {
			if content.Type == "tool_result" {
				if err := s.emit(Delta{ToolUseID: content.ToolUseID, ToolResult: string(content.Content), Failed: content.IsError}); err != nil {
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
			s.printed = true
			if err := s.emit(Delta{Text: full}); err != nil {
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

func (s *stream) validTool(name string) bool {
	if readTool(name) {
		return true
	}
	if !s.actions {
		return false
	}
	if !strings.HasPrefix(name, "mcp__") {
		return true
	}
	switch name {
	case "mcp__driga__answer_prompt", "mcp__driga__ack_inbox", "mcp__driga__jump", "mcp__driga__kill", "mcp__driga__cleanup", "mcp__driga__summarize", "mcp__driga__approve":
		return true
	}
	return false
}
