package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/self"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const providerCatalogLimit = 256 << 10

type providerTool struct {
	Name, Description string
	InputSchema       json.RawMessage
}
type providerToolResult struct {
	Text   string
	Failed bool
}

// providerTools is a turn-scoped, host-owned MCP connection. The provider sees
// only this catalog; it never receives the broker capability or an approve tool.
// Adapters must reject duplicate vendor call IDs before invoking Call.
type providerTools struct {
	session    *sdk.ClientSession
	connection *providerToolConnection
	catalog    []providerTool
	allowed    map[string]bool
	gate       chan struct{}
	calls      int
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan error
	closeOnce  sync.Once
	closeErr   error
	cleanup    func()
}

func openProviderTools(ctx context.Context, dataDir, executable string, r Request) (*providerTools, error) {
	if dataDir == "" {
		return nil, errors.New("provider tools require a data directory")
	}
	if !r.ReadOnly && (r.Socket == "" || r.Conversation == "" || r.Token == "") {
		return nil, errors.New("provider tool approval bridge unavailable")
	}
	env, err := mcpEnvironment(dataDir)
	if err != nil {
		return nil, err
	}
	env["DRIGA_CHAT_READ_ONLY"] = "1"
	if !r.ReadOnly {
		env["DRIGA_CHAT_READ_ONLY"] = "0"
		env["DRIGA_CHAT_SOCKET"], env["DRIGA_CHAT_CONVERSATION"], env["DRIGA_CHAT_TOKEN"] = r.Socket, r.Conversation, r.Token
	}
	if executable == "" {
		executable, err = self.Executable()
		if err != nil {
			return nil, err
		}
	}
	tmp := filepath.Join(env["DRIGA_DATA_DIR"], "tmp")
	if err := secfile.MkdirPrivate(tmp); err != nil {
		return nil, err
	}
	cwd, err := os.MkdirTemp(tmp, "provider-tools-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(cwd) }
	tools, err := startProviderTools(ctx, !r.ReadOnly, func(childCtx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(childCtx, executable, "mcp", "--chat")
		cmd.Dir = cwd
		cmd.Env = providerToolEnvironment(os.Environ(), env)
		return cmd
	})
	if err != nil {
		cleanup()
		return nil, err
	}
	tools.cleanup = cleanup
	return tools, nil
}

func providerToolEnvironment(inherited []string, overrides map[string]string) []string {
	env := make([]string, 0, len(inherited)+len(overrides)+1)
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "DRIGA_CHAT_") || upper == "DRIGA_INTERNAL" {
			continue
		}
		overridden := false
		for replacement := range overrides {
			if strings.EqualFold(key, replacement) {
				overridden = true
				break
			}
		}
		if !overridden {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return append(env, "DRIGA_INTERNAL=1")
}

// The factory is a test seam; production always starts our own mcp --chat.
func startProviderTools(ctx context.Context, actions bool, command func(context.Context) *exec.Cmd) (*providerTools, error) {
	childCtx, cancel := context.WithCancel(ctx)
	stdin, input, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, err
	}
	output, stdout, err := os.Pipe()
	if err != nil {
		cancel()
		stdin.Close()
		input.Close()
		return nil, err
	}
	cmd := command(childCtx)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, io.Discard
	tools := &providerTools{ctx: childCtx, cancel: cancel, done: make(chan error, 1), gate: make(chan struct{}, 1), allowed: map[string]bool{}}
	go func() {
		err := platform.RunChild(cmd)
		_ = stdin.Close()
		_ = stdout.Close()
		tools.done <- err
	}()
	transport := &providerToolTransport{IOTransport: sdk.IOTransport{Reader: output, Writer: input, MaxLineLength: maxLine}}
	client := sdk.NewClient(&sdk.Implementation{Name: "driga-provider", Version: "1"}, &sdk.ClientOptions{
		Capabilities: &sdk.ClientCapabilities{},
		// Tool execution must never be replayed by automatic input fulfillment.
		MultiRoundTrip: &sdk.MultiRoundTripOptions{Disabled: true},
	})
	startup, stopStartup := context.WithTimeout(childCtx, 10*time.Second)
	defer stopStartup()
	tools.session, err = client.Connect(startup, transport, nil)
	tools.connection = transport.connection
	if err == nil {
		err = tools.discover(startup, actions)
	}
	if err != nil {
		cancel()
		_ = output.Close()
		_ = input.Close()
		_ = tools.Close()
		return nil, fmt.Errorf("provider tools unavailable: %w", err)
	}
	return tools, nil
}

func (p *providerTools) discover(ctx context.Context, actions bool) error {
	allow := append([]string(nil), ReadTools...)
	if actions {
		allow = append(allow, "list_prompts", "run_command")
		allow = append(allow, DashboardWrites...)
	}
	cursor := ""
	cursors, names := map[string]bool{}, map[string]bool{}
	total, catalogBytes := 0, 0
	for page := 0; page < 8; page++ {
		result, err := p.session.ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return err
		}
		total += len(result.Tools)
		if total > 128 {
			return errors.New("provider tool catalog exceeds limit")
		}
		for _, tool := range result.Tools {
			if tool == nil || tool.Name == "" || names[tool.Name] {
				return errors.New("invalid provider tool catalog")
			}
			names[tool.Name] = true
			if !slices.Contains(allow, tool.Name) {
				continue
			}
			schema, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return err
			}
			if len(schema) == 0 || schema[0] != '{' {
				return errors.New("invalid provider tool schema")
			}
			catalogBytes += len(tool.Name) + len(tool.Description) + len(schema)
			if catalogBytes > providerCatalogLimit {
				return errors.New("provider tool catalog exceeds limit")
			}
			p.catalog = append(p.catalog, providerTool{tool.Name, tool.Description, schema})
			p.allowed[tool.Name] = true
		}
		if result.NextCursor == "" {
			if len(p.catalog) == 0 {
				return errors.New("no supported provider tools available")
			}
			return nil
		}
		if cursors[result.NextCursor] {
			return errors.New("provider tool catalog repeats a page")
		}
		cursor = result.NextCursor
		cursors[cursor] = true
	}
	return errors.New("provider tool catalog exceeds page limit")
}

func (p *providerTools) Catalog() []providerTool {
	tools := append([]providerTool(nil), p.catalog...)
	for i := range tools {
		tools[i].InputSchema = append(json.RawMessage(nil), tools[i].InputSchema...)
	}
	return tools
}

func (p *providerTools) Call(ctx context.Context, name string, arguments json.RawMessage) (providerToolResult, error) {
	var empty providerToolResult
	if !p.allowed[name] {
		return empty, fmt.Errorf("provider tool %q unavailable", name)
	}
	arguments = bytes.TrimSpace(arguments)
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if len(arguments) > MaxText || arguments[0] != '{' || !json.Valid(arguments) {
		return empty, errors.New("invalid or oversized provider tool arguments")
	}
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return empty, ctx.Err()
	case <-p.ctx.Done():
		return empty, p.ctx.Err()
	}
	defer func() { <-p.gate }()
	if err := p.ctx.Err(); err != nil {
		return empty, err
	}
	if p.calls >= 128 {
		return empty, errors.New("provider turn tool limit exceeded")
	}
	p.calls++
	result, err := p.session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return empty, err
	}
	if result.NeedsInput() {
		return empty, errors.New("provider tool requested unsupported input; call was not replayed")
	}
	// Keep the raw MCP envelope: decoding structuredContent through any can round
	// session IDs above 2^53. The SDK still validates the response and its status.
	raw := p.connection.result()
	if len(raw) == 0 || len(raw) > maxLine {
		return empty, errors.New("invalid or oversized provider tool result")
	}
	return providerToolResult{Text: string(raw), Failed: result.IsError}, nil
}

func (p *providerTools) Close() error {
	p.closeOnce.Do(func() {
		p.cancel()
		if p.session != nil {
			p.closeErr = p.session.Close()
		}
		<-p.done // RunChild reaps the owned group/job, including cancellation.
		if p.cleanup != nil {
			p.cleanup()
		}
	})
	return p.closeErr
}

type providerToolTransport struct {
	sdk.IOTransport
	connection *providerToolConnection
}

func (t *providerToolTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	c, err := t.IOTransport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.connection = &providerToolConnection{Connection: c}
	return t.connection, nil
}

type providerToolConnection struct {
	sdk.Connection
	mu     sync.Mutex
	callID jsonrpc.ID
	raw    json.RawMessage
}

func (c *providerToolConnection) Write(ctx context.Context, message jsonrpc.Message) error {
	if req, ok := message.(*jsonrpc.Request); ok && req.Method == "tools/call" {
		c.mu.Lock()
		c.callID, c.raw = req.ID, nil
		c.mu.Unlock()
	}
	return c.Connection.Write(ctx, message)
}
func (c *providerToolConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	message, err := c.Connection.Read(ctx)
	if response, ok := message.(*jsonrpc.Response); ok {
		c.mu.Lock()
		if response.ID == c.callID && c.raw == nil {
			c.raw = append(json.RawMessage(nil), response.Result...)
		}
		c.mu.Unlock()
	}
	return message, err
}
func (c *providerToolConnection) result() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append(json.RawMessage(nil), c.raw...)
}
