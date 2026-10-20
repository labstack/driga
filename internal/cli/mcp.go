package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	drigamcp "github.com/labstack/driga/internal/mcp"
	"github.com/labstack/driga/internal/view"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPCmd serves read-only tools over stdio.
type MCPCmd struct{}

// Help documents client registration without changing client configuration.
func (MCPCmd) Help() string {
	return "Read-only MCP server on stdio. Reading never acknowledges inbox items; snapshot mode has no history.\n\n" +
		"Claude Code: claude mcp add --scope user driga -- driga mcp\n\n" +
		"Codex configuration:\n[mcp_servers.driga]\ncommand = \"driga\"\nargs = [\"mcp\"]"
}

// Run reserves stdout for the MCP transport and sends diagnostics to stderr.
func (MCPCmd) Run(g *Globals) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := NewMCPServer(g).Run(ctx, &sdk.StdioTransport{MaxLineLength: drigamcp.MaxLineLength})
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// NewMCPServer shares the CLI read path with transport-independent tool handlers.
func NewMCPServer(g *Globals) *sdk.Server {
	base := *g
	stderr := base.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	base.Stderr = stderr
	read := func(ctx context.Context, q drigamcp.Query, build drigamcp.Builder) (view.Envelope, error) {
		copy := base
		copy.JSON, copy.Watch = true, 0
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
						v.Warning = "viewer collector not running"
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
			return Doctor(ctx, DoctorDeps{DataDir: config.DataDir(), Config: cfg, Home: config.ExpandHome("~"), CodexHome: os.Getenv("CODEX_HOME"), CopilotHome: os.Getenv("COPILOT_HOME"), Now: now, Store: r.(DoctorReader), LockProbe: probeCollectorLock}), nil
		})
	}
	// Neither the SDK logger nor snapshot hints may write to protocol stdout.
	return drigamcp.NewServer(drigamcp.Options{Version: base.Version, Logger: slog.New(slog.NewTextHandler(stderr, nil)), Read: read, Doctor: doctor, Cleanup: func(ctx context.Context, kind string, sizes bool) (view.Envelope, error) {
		return cleanupRead(ctx, &base, kind, sizes, false)
	}})
}
