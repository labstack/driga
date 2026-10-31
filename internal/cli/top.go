package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/tui"
	"github.com/labstack/driga/internal/view"
)

// TopCmd opens the interactive viewer using daemon data or live snapshots.
type TopCmd struct {
	TopOptions
	Tab string `enum:"flow,projects,machines" default:"flow" help:"Initial tab: flow, projects or machines."`
}

// Run keeps the viewer and its optional collector within the same lifetime.
func (c TopCmd) Run(g *Globals) error {
	input, output := g.Stdin, g.Stdout
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	in, inOK := input.(interface{ Fd() uintptr })
	out, outOK := output.(interface{ Fd() uintptr })
	if !inOK || !outOK || !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return invalidUsage("driga top needs an interactive terminal; use driga agents or --json")
	}
	overrides := map[string]any{}
	if g.Theme != nil {
		overrides["ui.theme"] = *g.Theme
	}
	cfg, err := config.LoadWithOverrides(config.Path(), overrides)
	if err != nil {
		return err
	}
	ctx, stop := notifyContext(context.Background())
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	source, _, closeSource, err := openViewer(ctx, g, cfg, view.Filter{})
	if err != nil {
		return err
	}
	defer closeSource()
	chatClient, ephemeral, closeChat, err := chat.Open(ctx, config.DataDir(), cfg.CaptureSnippets())
	if err != nil {
		return err
	}
	defer closeChat()
	opts := tui.Options{SessionAction: func(ctx context.Context, action string, id int64) error {
		raw, _ := json.Marshal(map[string]any{"session_id": id})
		actionGlobals := *g
		actionGlobals.Stdout = io.Discard
		return runChatAction(ctx, &actionGlobals, action, raw)
	}, InitialTab: c.Tab, Theme: cfg.UI.Theme, Chat: chatClient, ChatEphemeral: ephemeral, Machine: g.Machine, Project: g.Project, NoColor: g.NoColor, OpenCmd: cfg.OpenCmd, DataDir: config.DataDir(), Context: ctx}
	opts.Tmux = os.Getenv("TMUX")
	opts.JumpTerminal = cfg.Inbox.JumpTerminal
	opts.MotionOff = cfg.Motion == "off"
	if opts.Signatures, err = signatures.Load(cfg.ExtraSignatures); err != nil {
		return err
	}
	opts.CleanupList = func(ctx context.Context) ([]cleanup.Candidate, error) {
		envelope, err := cleanupRead(ctx, g, "", false, true)
		if err != nil {
			return nil, err
		}
		return envelope.Data.([]cleanup.Candidate), nil
	}
	opts.CleanupApply = func(ctx context.Context, c cleanup.Candidate) (cleanup.Result, error) {
		env, _, closeFn, err := openCleanup(ctx, g)
		if err != nil {
			return cleanup.Result{}, err
		}
		defer closeFn()
		return cleanup.Apply(ctx, env, c, cleanup.ApplyOpts{})
	}
	opts.Summarize = func(ctx context.Context, path string) error {
		local := *g
		local.Machine = "local"
		local.Project = ""
		local.JSON = false
		local.Stdout = io.Discard
		return (SummarizeCmd{Projects: []string{path}}).run(ctx, &local)
	}
	programOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if g.Stdin != nil {
		programOpts = append(programOpts, tea.WithInput(g.Stdin))
	}
	if g.Stdout != nil {
		programOpts = append(programOpts, tea.WithOutput(g.Stdout))
	}
	_, err = tea.NewProgram(tui.New(source, opts), programOpts...).Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}
