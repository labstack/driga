package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/driga/internal/llm"
	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/self"
	"github.com/pelletier/go-toml/v2"
)

// Codex uses exec's config-free mode, retaining CLI authentication. Disabling
// its execution environment removes native filesystem and command tools, with
// runtime backstops; commands are available only through Driga's broker.
type Codex struct{ Path, DataDir, Executable string }

func codexVersion(ctx context.Context, path string) error {
	version, err := providerMetadata(ctx, path, []string{"--version"})
	if err != nil {
		return err
	}
	// The environment-disabled runtime was audited against this exact release.
	// Fail closed for unknown tool registries rather than assume flag parity.
	fields := strings.Fields(strings.TrimSpace(version))
	if len(fields) != 2 || fields[0] != "codex-cli" || fields[1] != "0.167.0" {
		return errors.New("Codex assistant requires the audited 0.167.0 CLI release; other builds are not yet supported")
	}
	return nil
}

func (a *Codex) Run(ctx context.Context, r Request, emit func(Delta) error) (err error) {
	if err := codexVersion(ctx, a.Path); err != nil {
		return err
	}
	if r.Resume != "" {
		if _, err := uuid.Parse(r.Resume); err != nil {
			return errors.New("invalid Codex resume session")
		}
	}
	exe := a.Executable
	if exe == "" {
		exe, err = self.Executable()
		if err != nil {
			return err
		}
	}
	env, err := mcpEnvironment(a.DataDir)
	if err != nil {
		return err
	}
	tools := append([]string{}, ReadTools...)
	actions := !r.ReadOnly && r.Socket != "" && r.Token != ""
	env["DRIGA_CHAT_READ_ONLY"] = "1"
	if actions {
		env["DRIGA_CHAT_READ_ONLY"] = "0"
		env["DRIGA_CHAT_SOCKET"], env["DRIGA_CHAT_CONVERSATION"], env["DRIGA_CHAT_TOKEN"] = r.Socket, r.Conversation, r.Token
		tools = append(tools, "list_prompts", "run_command")
	}
	// A scratch cwd prevents project configuration or instructions discovery.
	// The selected project stays broker-owned; run_command resolves it there.
	if err := secfile.MkdirPrivate(a.DataDir); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(a.DataDir, "codex-chat-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var envNames, extraEnv []string
	for key, value := range env {
		envNames = append(envNames, key)
		extraEnv = append(extraEnv, key+"="+value)
	}
	sort.Strings(envNames)
	extraEnv = append(extraEnv, "CODEX_EXEC_SERVER_URL=none")
	conf := map[string]any{
		"mcp_servers":     map[string]any{"driga": map[string]any{"command": exe, "args": []string{"mcp", "--chat"}, "env_vars": envNames, "enabled_tools": tools, "required": true}},
		"approval_policy": "never", "sandbox_mode": "read-only", "web_search": "disabled", "project_doc_max_bytes": 0,
		"notify": []string{}, "agents": map[string]any{"enabled": false},
		"skills":   map[string]any{"bundled": map[string]any{"enabled": false}, "include_instructions": false},
		"features": map[string]any{"hooks": false, "plugins": false, "plugin_hooks": false, "apps": false, "shell_tool": false, "unified_exec": false, "js_repl": false, "multi_agent": false, "multi_agent_v2": false, "memories": false, "image_generation": false, "in_app_browser": false, "request_permissions_tool": false},
	}
	args := []string{"exec", "--ignore-user-config", "--ignore-rules", "--strict-config", "--json", "--skip-git-repo-check"}
	// Marshal one complete TOML value per override. Do not shell-escape arguments:
	// these are passed directly to exec. Broker credentials stay out of argv.
	for _, key := range []string{"mcp_servers", "approval_policy", "sandbox_mode", "web_search", "project_doc_max_bytes", "notify", "agents", "skills", "features"} {
		encoded, e := toml.Marshal(map[string]any{"value": conf[key]})
		if e != nil {
			return e
		}
		var parsed map[string]any
		if e = toml.Unmarshal(encoded, &parsed); e != nil {
			return e
		}
		value, e := codexTOMLValue(parsed["value"])
		if e != nil {
			return e
		}
		args = append(args, "-c", key+"="+value)
	}
	if r.Resume != "" {
		args = append(args, "resume", r.Resume)
	}
	args = append(args, "-")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	allowed := make(map[string]bool, len(tools))
	for _, tool := range tools {
		allowed[tool] = true
	}
	s := codexStream{emit: emit, cancel: cancel, resume: r.Resume, allowed: allowed, calls: map[string]string{}, finished: map[string]bool{}}
	s.cards = stream{emit: emit, cancel: cancel}
	defer func() { err = errors.Join(err, s.cards.flushCards()) }()
	err = llm.RunProvider(ctx, a.Path, dir, args, extraEnv, strings.NewReader(r.Prompt), &s, io.Discard)
	if s.err != nil {
		return s.err
	}
	if err != nil {
		return err
	}
	if len(s.buf) != 0 || !s.started || !s.completed {
		return errors.New("Codex stream ended without a complete turn")
	}
	return nil
}

// JSON strings and numbers are valid TOML scalars; arrays and inline tables
// need their own separators. Keys are always quoted, including environment keys.
func codexTOMLValue(value any) (string, error) {
	switch v := value.(type) {
	case map[string]any:
		var items []string
		for key, item := range v {
			s, err := codexTOMLValue(item)
			if err != nil {
				return "", err
			}
			raw, _ := json.Marshal(key)
			items = append(items, string(raw)+"="+s)
		}
		return "{" + strings.Join(items, ",") + "}", nil
	case []any:
		var items []string
		for _, item := range v {
			s, err := codexTOMLValue(item)
			if err != nil {
				return "", err
			}
			items = append(items, s)
		}
		return "[" + strings.Join(items, ",") + "]", nil
	default:
		raw, err := json.Marshal(v)
		return string(raw), err
	}
}

type codexStream struct {
	emit               func(Delta) error
	cancel             context.CancelFunc
	buf                []byte
	err                error
	resume             string
	started, completed bool
	allowed            map[string]bool
	calls              map[string]string
	finished           map[string]bool
	cards              stream
}

func (s *codexStream) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	for _, b := range p {
		if b == '\n' {
			s.err = s.line(s.buf)
			s.buf = s.buf[:0]
			if s.err != nil {
				s.cancel()
				return 0, s.err
			}
		} else {
			if len(s.buf) >= 1<<20 {
				s.err = errors.New("Codex event exceeds limit")
				s.cancel()
				return 0, s.err
			}
			s.buf = append(s.buf, b)
		}
	}
	return len(p), nil
}
func (s *codexStream) line(line []byte) error {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	var e struct {
		Type, ThreadID string
		Thread         string `json:"thread_id"`
		Item           struct {
			ID, Type, Text, Server, Tool, Status string
			Arguments                            json.RawMessage
			Result                               struct {
				Content json.RawMessage
				IsError bool `json:"isError"`
			}
		}
		Error json.RawMessage
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return errors.New("invalid Codex event JSON")
	}
	if s.completed {
		return errors.New("Codex event after completed turn")
	}
	switch e.Type {
	case "thread.started":
		if s.started {
			return errors.New("duplicate Codex session")
		}
		if _, err := uuid.Parse(e.Thread); err != nil {
			return errors.New("invalid Codex session ID")
		}
		if s.resume != "" && e.Thread != s.resume {
			return errors.New("Codex resumed a different session")
		}
		s.started = true
		return s.emit(Delta{SessionID: e.Thread})
	case "turn.started":
		if !s.started {
			return errors.New("Codex turn before session")
		}
	case "item.started", "item.updated", "item.completed":
		if !s.started {
			return errors.New("Codex item before session")
		}
		switch e.Item.Type {
		case "agent_message":
			if e.Type == "item.completed" {
				return s.emit(Delta{Text: e.Item.Text})
			}
		case "mcp_tool_call":
			if e.Item.Server != "driga" {
				return errors.New("Codex called an unexpected MCP server")
			}
			if !s.allowed[e.Item.Tool] {
				return errors.New("Codex called a disabled Driga tool")
			}
			if e.Item.ID == "" || len(e.Item.ID) > 256 {
				return errors.New("invalid Codex tool call ID")
			}
			if s.calls == nil {
				s.calls = map[string]string{}
				s.finished = map[string]bool{}
			}
			previous, exists := s.calls[e.Item.ID]
			if s.finished[e.Item.ID] || exists && (previous != e.Item.Tool || e.Type == "item.started") {
				return errors.New("duplicate Codex tool call ID")
			}
			if !exists {
				if len(s.calls) >= 256 {
					return errors.New("Codex tool call limit exceeded")
				}
				s.calls[e.Item.ID] = e.Item.Tool
				if err := s.cards.panel(e.Item.ID, "mcp__driga__"+e.Item.Tool, e.Item.Arguments); err != nil {
					return err
				}
			}
			if e.Type == "item.started" {
				return s.emit(Delta{Tool: e.Item.Tool})
			}
			if e.Type == "item.completed" {
				s.finished[e.Item.ID] = true
				return s.cards.settle(e.Item.ID, e.Item.Result.Content, e.Item.Result.IsError || e.Item.Status == "failed")
			}
		case "reasoning", "todo_list":
		default:
			return fmt.Errorf("unexpected Codex native tool or item %q", e.Item.Type)
		}
	case "turn.completed":
		if !s.started {
			return errors.New("Codex completed before session")
		}
		for id := range s.calls {
			if !s.finished[id] {
				return errors.New("Codex completed with an unfinished tool call")
			}
		}
		s.completed = true
	case "turn.failed", "error":
		return errors.New("Codex turn failed; check CLI authentication and availability")
	default:
		return fmt.Errorf("unsupported Codex event %q", e.Type)
	}
	return nil
}
