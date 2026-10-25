package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/dashboard"
	drigamcp "github.com/labstack/driga/internal/mcp"
	"github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/view"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPCmd serves read-only tools over stdio.
type MCPCmd struct {
	Snapshot bool `help:"Force one-shot snapshot collection for read tools (no history)."`
	Chat     bool `hidden:"" help:"Apply Chat capture policy to every response."`
}

func (c *MCPCmd) AfterApply(g *Globals) error { g.Snapshot = c.Snapshot; return nil }

// Help documents client registration without changing client configuration.
func (MCPCmd) Help() string {
	return "Read-only MCP server on stdio. Reading never acknowledges inbox items; snapshot mode has no history.\n\n" +
		"Claude Code: claude mcp add --scope user driga -- driga mcp\n\n" +
		"Codex configuration:\n[mcp_servers.driga]\ncommand = \"driga\"\nargs = [\"mcp\"]"
}

// Run reserves stdout for the MCP transport and sends diagnostics to stderr.
func (c MCPCmd) Run(g *Globals) error {
	ctx, stop := notifyContext(context.Background())
	defer stop()
	policy := ""
	if c.Chat {
		cfg, err := config.Load(config.Path())
		if err != nil {
			return err
		}
		policy = cfg.CaptureText
	}
	server := newMCPServer(g, c.Chat, c.Chat && os.Getenv("DRIGA_CHAT_READ_ONLY") != "1", policy)
	err := server.Run(ctx, &sdk.StdioTransport{MaxLineLength: drigamcp.MaxLineLength})
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// NewMCPServer shares the CLI read path with transport-independent tool handlers.
func NewMCPServer(g *Globals, capture ...string) *sdk.Server {
	policy := ""
	if len(capture) > 0 {
		policy = capture[0]
	}
	return newMCPServer(g, false, false, policy)
}

// newMCPServer serves an outside MCP client, or Chat when forChat is set, with
// its actions when actions is.
func newMCPServer(g *Globals, forChat, actions bool, capture ...string) *sdk.Server {
	policy := ""
	if len(capture) > 0 {
		policy = capture[0]
	}
	base := *g
	stderr := base.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	base.Stderr = stderr
	read := func(ctx context.Context, q drigamcp.Query, build drigamcp.Builder) (view.Envelope, error) {
		copy := base
		copy.JSON = true
		copy.cachedMachines = q.CachedMachines
		if q.Machine != "" {
			copy.Machine = q.Machine
		}
		if q.Since != "" {
			copy.Since = q.Since
		}
		var out bytes.Buffer
		copy.Stdout = &out
		// readOnce resolves scopes and builds the envelope in one transaction.
		// Reusing its JSON also preserves CLI null/empty and numeric contracts.
		err := readOnce(ctx, &copy, q.Filter, q.Project, func(r view.Reader, f view.Filter, now time.Time) (any, error) {
			data, err := build(r, f, now)
			if q.CachedMachines && !copy.Snapshot {
				if v, ok := data.(view.MachinesView); ok {
					if !collector.Running(config.DataDir()) {
						v.Warning = "this machine's Driga service is not running"
					}
					data = v
				}
			}
			return data, err
		})
		if err != nil {
			return view.Envelope{}, err
		}
		var envelope view.Envelope
		decoder := json.NewDecoder(&out)
		decoder.UseNumber()
		err = decoder.Decode(&envelope)
		return envelope, err
	}
	doctor := func(ctx context.Context) (view.Envelope, error) {
		cfg, err := config.Load(config.Path())
		if err != nil {
			return view.Envelope{}, err
		}
		return read(ctx, drigamcp.Query{CachedMachines: true}, func(r view.Reader, _ view.Filter, now time.Time) (any, error) {
			return runtimeDoctor(ctx, cfg, now, r.(DoctorReader)), nil
		})
	}
	var prompts func(context.Context) ([]chat.Prompt, error)
	if actions {
		prompts = func(ctx context.Context) ([]chat.Prompt, error) {
			in, err := chatApproval(json.RawMessage(`{}`))
			if err != nil {
				return nil, err
			}
			return chat.ChatPrompts(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), in)
		}
	}
	// Neither the SDK logger nor snapshot hints may write to protocol stdout.
	return drigamcp.NewServer(drigamcp.Options{Prompts: prompts, Chat: actions, ChatServer: forChat, Action: chatAction(&base), Approve: chatApprove, CaptureText: policy, Version: base.Version, Logger: slog.New(slog.NewTextHandler(stderr, nil)), Read: read, Doctor: doctor, Cleanup: func(ctx context.Context, kind string, sizes bool) (view.Envelope, error) {
		return cleanupRead(ctx, &base, kind, sizes, false)
	}, Panels: func(ctx context.Context, req panel.RunRequest) (panel.Fragment, error) {
		return runPanels(ctx, &base, req)
	}, Dashboards: dashboard.New(config.DataDir())})
}
