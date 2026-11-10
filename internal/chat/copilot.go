package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/driga/internal/llm"
	"github.com/labstack/driga/internal/secfile"
)

// Copilot uses the SDK's headless protocol, with host-owned tools only. ACP's
// merged MCP configuration cannot provide the same configuration ceiling.
type Copilot struct {
	Path, DataDir, Executable string
	openTools                 func(context.Context, string, string, Request) (copilotTools, error)
}

type copilotTools interface {
	Catalog() []providerTool
	Call(context.Context, string, json.RawMessage) (providerToolResult, error)
	Close() error
}

func (a *Copilot) Run(ctx context.Context, r Request, emit func(Delta) error) (err error) {
	if a.Path == "" {
		return errors.New("copilot executable is not pinned")
	}
	openTools := a.openTools
	if openTools == nil {
		openTools = func(ctx context.Context, dataDir, executable string, r Request) (copilotTools, error) {
			return openProviderTools(ctx, dataDir, executable, r)
		}
	}
	bridge, err := openTools(ctx, a.DataDir, a.Executable, r)
	if err != nil {
		return err
	}
	defer bridge.Close()
	configDir := filepath.Join(a.DataDir, "assistant", "copilot")
	if err := secfile.MkdirPrivate(configDir); err != nil {
		return err
	}
	root := filepath.Join(a.DataDir, "tmp")
	if err := secfile.MkdirPrivate(root); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, "copilot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	input, writeInput := io.Pipe()
	output, writeOutput := io.Pipe()
	defer input.Close()
	defer writeInput.Close()
	defer output.Close()
	defer writeOutput.Close()
	done := make(chan error, 1)
	go func() {
		err := llm.RunProvider(ctx, a.Path, dir, []string{"--headless", "--stdio", "--no-auto-update", "--disable-builtin-mcps", "--no-custom-instructions", "--no-bash-env"}, nil, input, writeOutput, io.Discard)
		writeOutput.CloseWithError(err)
		done <- err
	}()
	defer func() { cancel(); writeInput.Close(); output.Close(); <-done }()
	startup := time.AfterFunc(30*time.Second, cancel)
	defer startup.Stop()
	p := copilotRPC{ctx: ctx, reader: bufio.NewReaderSize(output, 4096), writer: writeInput, emit: emit, bridge: bridge, seen: map[string]bool{}, pending: map[string]bool{}, responses: map[string]copilotMessage{}}
	p.cards = stream{emit: emit, cancel: cancel}
	defer func() { err = errors.Join(err, p.cards.flushCards()) }()
	var connect struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err := p.call("connect", map[string]any{"clientInfo": map[string]string{"name": "driga"}}, &connect); err != nil {
		return err
	}
	if connect.ProtocolVersion != 3 {
		return fmt.Errorf("copilot requires SDK protocol 3, got %d", connect.ProtocolVersion)
	}
	var declarations []map[string]any
	p.allowed = map[string]bool{}
	var names []string
	for _, t := range bridge.Catalog() {
		declarations = append(declarations, map[string]any{"name": t.Name, "description": t.Description, "parameters": t.InputSchema, "hasHandler": true})
		names = append(names, t.Name)
		p.allowed[t.Name] = true
	}
	options := map[string]any{
		"clientName": "driga-internal", "workingDirectory": dir, "configDir": configDir,
		"availableTools": names, "tools": declarations, "streaming": true,
		"mcpServers": map[string]any{}, "customAgents": []any{}, "installedPlugins": []any{},
		"skillDirectories": []string{}, "agentDirectories": []string{}, "hookDirectories": []string{},
		"pluginDirectories": []string{}, "instructionDirectories": []string{}, "includedBuiltinSkills": []string{},
		"enableConfigDiscovery": false, "enableFileHooks": false, "enableHostGitOperations": false,
		"enableSkills": false, "enableOnDemandInstructionDiscovery": false, "skipCustomInstructions": true,
		"enableSessionTelemetry": false, "enableSessionStore": false, "isExperimentalMode": false,
		"skipEmbeddingRetrieval": true, "embeddingCacheStorage": "in-memory", "mcpOAuthTokenStorage": "in-memory",
		"customAgentsLocalOnly": true, "coauthorEnabled": false, "manageScheduleEnabled": false,
		"memory": map[string]bool{"enabled": false}, "requestPermission": true,
	}
	method := "session.create"
	p.session = uuid.NewString()
	options["sessionId"] = p.session
	if r.Resume != "" {
		if !validCopilotID(r.Resume) {
			return errors.New("invalid copilot session ID")
		}
		method, options["sessionId"] = "session.resume", r.Resume
		p.session = r.Resume
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err := p.call(method, options, &session); err != nil {
		return err
	}
	if !validCopilotID(session.SessionID) || session.SessionID != p.session {
		return errors.New("copilot returned an unexpected session ID")
	}
	p.session = session.SessionID
	patch := map[string]any{"sessionId": p.session, "skipCustomInstructions": true, "customAgentsLocalOnly": true, "coauthorEnabled": false, "manageScheduleEnabled": false, "installedPlugins": []any{}, "includedBuiltinSkills": []string{}, "availableTools": names, "enableFileHooks": false, "enableSkills": false}
	var updated struct {
		Success         bool `json:"success"`
		PluginHookCount *int `json:"pluginHookCount"`
	}
	if err := p.call("session.options.update", patch, &updated); err != nil {
		return err
	}
	if !updated.Success || updated.PluginHookCount == nil || *updated.PluginHookCount != 0 {
		return errors.New("copilot did not establish isolated session options")
	}
	if err := p.call("session.tools.initializeAndValidate", map[string]string{"sessionId": p.session}, nil); err != nil {
		return err
	}
	var metadata struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := p.call("session.tools.getCurrentMetadata", map[string]string{"sessionId": p.session}, &metadata); err != nil {
		return err
	}
	if metadata.Tools == nil || len(metadata.Tools) != len(names) {
		return errors.New("copilot tool catalog does not match Driga's tools")
	}
	listed := map[string]bool{}
	for _, tool := range metadata.Tools {
		if !p.allowed[tool.Name] || listed[tool.Name] {
			return fmt.Errorf("copilot offered an unexpected tool %q", tool.Name)
		}
		listed[tool.Name] = true
	}
	if err := emit(Delta{SessionID: p.session}); err != nil {
		return err
	}
	if !startup.Stop() || ctx.Err() != nil {
		return errors.New("copilot startup timed out")
	}
	p.started = true
	if err := p.call("session.send", map[string]any{"sessionId": p.session, "prompt": r.Prompt}, nil); err != nil {
		return err
	}
	for !p.finished {
		message, err := p.read()
		if err != nil {
			return err
		}
		if err := p.event(message); err != nil {
			return err
		}
	}
	return ctx.Err()
}

type copilotMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type copilotRPC struct {
	ctx                        context.Context
	reader                     *bufio.Reader
	writer                     io.Writer
	next                       int
	session                    string
	started, finished, partial bool
	allowed, seen              map[string]bool
	pending                    map[string]bool
	responses                  map[string]copilotMessage
	bridge                     copilotTools
	emit                       func(Delta) error
	cards                      stream
}

func (p *copilotRPC) write(value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > maxLine {
		return errors.New("copilot request exceeds limit")
	}
	_, err = fmt.Fprintf(p.writer, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (p *copilotRPC) read() (copilotMessage, error) {
	var message copilotMessage
	length := -1
	for total := 0; ; {
		chunk, err := p.reader.ReadSlice('\n')
		line := string(chunk)
		total += len(line)
		if total > 4096 {
			return message, errors.New("copilot header exceeds limit")
		}
		if err != nil {
			return message, fmt.Errorf("copilot stream: %w", err)
		}
		if line == "\r\n" {
			break
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			return message, errors.New("invalid copilot header")
		}
		if strings.EqualFold(key, "Content-Length") {
			if length != -1 {
				return message, errors.New("duplicate copilot content length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length < 0 || length > maxLine {
				return message, errors.New("invalid copilot content length")
			}
		}
	}
	if length < 0 {
		return message, errors.New("missing copilot content length")
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(p.reader, b); err != nil {
		return message, err
	}
	if err := json.Unmarshal(b, &message); err != nil {
		return message, err
	}
	if message.JSONRPC != "2.0" {
		return message, errors.New("invalid copilot JSON-RPC version")
	}
	return message, nil
}

func (p *copilotRPC) call(method string, params any, result any) error {
	p.next++
	id := strconv.Itoa(p.next)
	p.pending[id] = true
	defer delete(p.pending, id)
	if err := p.write(map[string]any{"jsonrpc": "2.0", "id": p.next, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		message, cached := p.responses[id]
		delete(p.responses, id)
		var err error
		if !cached {
			message, err = p.read()
		}
		if err != nil {
			return err
		}
		if message.Method != "" {
			if err := p.event(message); err != nil {
				return err
			}
			continue
		}
		if string(message.ID) != id {
			other := string(message.ID)
			if !p.pending[other] || len(p.responses) >= 64 {
				return errors.New("unexpected copilot response ID")
			}
			if _, exists := p.responses[other]; exists {
				return errors.New("duplicate copilot response ID")
			}
			p.responses[other] = message
			continue
		}
		if len(message.Error) != 0 && string(message.Error) != "null" {
			return fmt.Errorf("copilot %s failed", method)
		}
		if result != nil {
			return json.Unmarshal(message.Result, result)
		}
		return nil
	}
}

func (p *copilotRPC) event(message copilotMessage) error {
	if len(message.ID) != 0 {
		// No native permission, extension or user-input callback can grant authority.
		return errors.New("copilot requested an unsupported host capability")
	}
	if message.Method != "session.event" {
		return nil
	}
	var notice struct {
		SessionID string `json:"sessionId"`
		Event     struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		} `json:"event"`
	}
	if err := json.Unmarshal(message.Params, &notice); err != nil {
		return err
	}
	if p.session == "" || notice.SessionID != p.session {
		return errors.New("unexpected copilot event session")
	}
	var data struct {
		DeltaContent string          `json:"deltaContent"`
		Content      string          `json:"content"`
		RequestID    string          `json:"requestId"`
		ToolName     string          `json:"toolName"`
		ToolCallID   string          `json:"toolCallId"`
		Arguments    json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(notice.Event.Data, &data); err != nil {
		return err
	}
	switch notice.Event.Type {
	case "assistant.message_delta":
		if !p.started {
			return errors.New("copilot generated output before prompt")
		}
		if !p.partial {
			p.cards.gap = p.cards.printed
		}
		p.partial = true
		return p.cards.text(data.DeltaContent)
	case "assistant.message":
		if !p.started {
			return errors.New("copilot generated output before prompt")
		}
		if !p.partial {
			p.cards.gap = p.cards.printed
			return p.cards.text(data.Content)
		}
		p.partial = false
	case "session.idle":
		if p.started {
			p.finished = true
		}
	case "session.error":
		return errors.New("copilot turn failed")
	case "permission.requested":
		return errors.New("copilot requested native permission")
	case "external_tool.requested":
		if !p.started || !p.allowed[data.ToolName] || !validCopilotID(data.RequestID) || !validCopilotID(data.ToolCallID) || p.seen["request:"+data.RequestID] || p.seen["call:"+data.ToolCallID] || len(p.seen) >= 2048 {
			return errors.New("invalid copilot tool call")
		}
		p.seen["request:"+data.RequestID] = true
		p.seen["call:"+data.ToolCallID] = true
		if err := p.emit(Delta{Tool: "mcp__driga__" + data.ToolName}); err != nil {
			return err
		}
		if err := p.cards.panel(data.ToolCallID, "mcp__driga__"+data.ToolName, data.Arguments); err != nil {
			return err
		}
		result, err := p.bridge.Call(p.ctx, data.ToolName, data.Arguments)
		if err != nil {
			return err
		}
		if err := p.emit(Delta{ToolUseID: data.ToolCallID, ToolResult: result.Text, Failed: result.Failed}); err != nil {
			return err
		}
		var envelope struct {
			Content json.RawMessage `json:"content"`
		}
		_ = json.Unmarshal([]byte(result.Text), &envelope)
		if err := p.cards.settle(data.ToolCallID, envelope.Content, result.Failed); err != nil {
			return err
		}
		params := map[string]any{"sessionId": p.session, "requestId": data.RequestID, "result": result.Text}
		if result.Failed {
			delete(params, "result")
			params["error"] = result.Text
		}
		var accepted struct {
			Success bool `json:"success"`
		}
		if err := p.call("session.tools.handlePendingToolCall", params, &accepted); err != nil {
			return err
		}
		if !accepted.Success {
			return errors.New("copilot rejected tool result")
		}
	}
	return nil
}

func validCopilotID(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, c := range id {
		if c < 0x21 || c > 0x7e || c == '/' || c == '\\' {
			return false
		}
	}
	return true
}
