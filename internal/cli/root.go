// Package cli defines driga's kong command tree.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/scrub"
)

// Globals are flags shared by every command.
type Globals struct {
	// CleanupPlatform injects read-only inspection for embedded callers/tests.
	CleanupPlatform platform.Platform `kong:"-"`
	Machine         string            `default:"all" help:"Scope views to a machine name, ID, device-ID prefix, local, or all."`
	cachedMachines  bool              `kong:"-"`
	JSON            bool              `help:"Print versioned JSON instead of tables."`
	Project         string            `help:"Scope views to one project (path, name, or '.')."`
	Since           string            `help:"Only show items since a duration ago or 'yesterday'."`
	Snapshot        bool              `help:"Force snapshot mode (one-shot collect, no history)."`
	NoColor         bool              `name:"no-color" help:"Disable colors."`
	Watch           time.Duration     `help:"Re-render every interval (e.g. 2s)." placeholder:"DUR"`

	Version string    `kong:"-"`
	Commit  string    `kong:"-"`
	Date    string    `kong:"-"`
	Stdout  io.Writer `kong:"-"`
	Stderr  io.Writer `kong:"-"`
	Stdin   io.Reader `kong:"-"`
}

// Root is the full command tree.
type Root struct {
	Globals

	Summary   SummaryCmd     `cmd:"" default:"1" help:"Show a one-screen summary."`
	Flow      FlowCmd        `cmd:"" help:"Show a session family and its state signals."`
	Activity  ActivityCmd    `cmd:"" help:"Show the last hour of machine activity."`
	Agents    AgentsCmd      `cmd:"" help:"List agents grouped by family."`
	Agent     AgentCmd       `cmd:"" help:"Show session detail."`
	Projects  ProjectsCmd    `cmd:"" help:"List project status."`
	Summarize SummarizeCmd   `cmd:"" help:"Refresh opt-in local project summaries through Claude."`
	Ask       AskCmd         `cmd:"" help:"Ask a one-shot Chat question through Claude and driga's read-only tools."`
	Cleanup   CleanupCmd     `cmd:"" help:"Inspect and explicitly clean local agent disk candidates."`
	Where     WhereCmd       `cmd:"" help:"Show where a project stands."`
	Inbox     InboxCmd       `cmd:"" help:"List attention and completion events."`
	Jump      JumpCmd        `cmd:"" help:"Jump to a local inbox item without acknowledging it."`
	Conflicts ConflictsCmd   `cmd:"" help:"Show current and recent overlaps."`
	Orphans   OrphansCmd     `cmd:"" help:"List orphan process trees."`
	Kill      OrphansKillCmd `cmd:"" help:"Confirm and kill driga-proven local orphan trees (alias of orphans kill)."`
	Machines  MachinesCmd    `cmd:"" help:"List known machines and their connection health."`
	Machine   MachineCmd     `cmd:"" help:"Show machine health."`
	Web       WebCmd         `cmd:"" help:"Open a read-only browser dashboard on loopback."`
	Top       TopCmd         `cmd:"" help:"Open the interactive session viewer."`
	MCP       MCPCmd         `cmd:"" name:"mcp" help:"Serve read-only MCP tools over stdio."`

	Service    ServiceCmd `cmd:"" help:"Manage the collector service."`
	Doctor     DoctorCmd  `cmd:"" help:"Check setup and collection health."`
	Init       InitCmd    `cmd:"" help:"Guide optional setup."`
	Config     ConfigCmd  `cmd:"" help:"Inspect or edit configuration."`
	Collect    CollectCmd `cmd:"" help:"Run the collector (what launchd runs)."`
	Serve      ServeCmd   `cmd:"" help:"Run the collector with fleet serving enabled."`
	Fleet      FleetCmd   `cmd:"" help:"Inspect fleet identity, serving status and viewers."`
	Hook       HookCmd    `cmd:"" hidden:"" help:"Record an agent hook."`
	Hooks      HooksCmd   `cmd:"" help:"Manage agent hooks."`
	Feed       FeedCmd    `cmd:"" hidden:"" help:"Inspect the local fleet feed."`
	Dev        DevCmd     `cmd:"" hidden:"" help:"Inspect unsupported agent formats."`
	VersionCmd VersionCmd `cmd:"" name:"version" help:"Print the driga version."`
}

// VersionCmd prints the version and release build metadata.
type VersionCmd struct{}

func (VersionCmd) Run(g *Globals) error {
	if g.JSON {
		return json.NewEncoder(commandOutput(g)).Encode(map[string]any{"schema": 2, "data": map[string]string{
			"version": g.Version, "commit": g.Commit, "date": g.Date,
			"go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		}})
	}
	_, err := fmt.Fprintf(commandOutput(g), "driga %s\ncommit: %s\ndate: %s\ngo: %s\nos/arch: %s/%s\n",
		g.Version, g.Commit, g.Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return err
}

// Main parses args, runs the selected command and returns the exit code.
func Main(args []string, version string, metadata ...string) int {
	if err := config.ResolvePaths(); err != nil {
		fmt.Fprintln(os.Stderr, "driga: resolve paths:", err)
		return 1
	}
	return run(args, version, os.Stdin, os.Stdout, os.Stderr, metadata...)
}

func run(args []string, version string, stdin io.Reader, stdout, stderr io.Writer, metadata ...string) int {
	// Hooks are synchronous agent callbacks. Bypass the interactive command
	// parser and its usage output; every hook invocation must be quiet and fail open.
	if len(args) > 0 && args[0] == "hook" {
		if len(args) < 3 {
			hookLogError(errors.New("hook requires adapter and event"))
			return 0
		}
		_ = (HookCmd{Adapter: args[1], Event: args[2], Payload: args[3:]}).Run(&Globals{Stdin: stdin})
		return 0
	}

	var root Root
	root.Version, root.Stdout, root.Stderr, root.Stdin = version, stdout, stderr, stdin
	root.Commit, root.Date = "unknown", "unknown"
	if len(metadata) > 0 {
		root.Commit = metadata[0]
	}
	if len(metadata) > 1 {
		root.Date = metadata[1]
	}
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
			jsonMode = true
		}
		if arg == "--json=false" {
			jsonMode = false
		}
	}
	report := func(err error, code int) {
		fmt.Fprintln(stderr, "driga:", err)
		if jsonMode {
			var reported reportedError
			if errors.As(err, &reported) {
				return
			}
			name := "runtime_error"
			if code == 2 {
				name = "usage_error"
			}
			if strings.Contains(err.Error(), "--json is not supported") {
				name = "unsupported_output"
			}
			var missing notFoundError
			if errors.As(err, &missing) {
				name = "not_found"
			}
			_ = json.NewEncoder(stdout).Encode(map[string]any{"schema": 2, "error": map[string]string{"code": name, "message": scrub.Scrub(err.Error())}})
		}
	}
	exit := -1
	parser, err := kong.New(&root,
		kong.Name("driga"),
		kong.Description("Watch every AI coding agent session on this machine."),
		kong.Writers(stdout, stderr),
		kong.Help(commandHelp),
		kong.Exit(func(code int) { exit = code; panic(exitPanic{}) }),
		kong.Bind(&root.Globals),
	)
	if err != nil {
		report(err, 2)
		return 2
	}
	code := func() (code int) {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(exitPanic); ok {
					code = exit
					return
				}
				panic(r)
			}
		}()
		ctx, err := parser.Parse(args)
		if err != nil {
			report(err, 2)
			return 2
		}
		if root.JSON && !supportsJSON(ctx.Command()) {
			err := fmt.Errorf("%s: --json is not supported", ctx.Command())
			report(err, 2)
			return 2
		}
		if err := ctx.Run(); err != nil {
			code := 1
			var exitErr interface{ ExitCode() int }
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			}
			if strings.Contains(err.Error(), "--json is not supported") {
				code = 2
			}
			report(err, code)
			return code
		}
		return 0
	}()
	return code
}

type exitPanic struct{}

// reportedError preserves a structured partial mutation result already written.
type reportedError struct{ error }

// Reject unsupported formats before starting interactive commands or mutations.
func supportsJSON(command string) bool {
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "flow", "summary", "activity", "agents", "agent", "projects", "where", "conflicts", "machines", "cleanup", "ask", "summarize", "doctor", "version", "dev", "feed":
		return true
	case "inbox", "orphans", "machine":
		return len(words) < 2 || words[1] == "list" || words[1] == "show"
	case "config":
		return len(words) > 1 && words[1] == "path"
	case "fleet":
		return len(words) > 1 && (words[1] == "id" || words[1] == "status" || words[1] == "viewers" || words[1] == "peers" || words[1] == "add")
	}
	return false
}

type usageError struct{ error }

func (usageError) ExitCode() int                    { return 2 }
func invalidUsage(format string, args ...any) error { return usageError{fmt.Errorf(format, args...)} }

// commandHelp keeps global parsing compatible while showing only supported output.
func commandHelp(options kong.HelpOptions, ctx *kong.Context) error {
	selected := ctx.Selected()
	if selected == nil || supportsJSON(ctx.Command()) {
		return kong.DefaultHelpPrinter(options, ctx)
	}
	for _, flag := range ctx.Model.Flags {
		if flag.Name == "json" {
			hidden := flag.Hidden
			flag.Hidden = true
			defer func() { flag.Hidden = hidden }()
		}
	}
	return kong.DefaultHelpPrinter(options, ctx)
}

type notFoundError struct{ error }

func missingAgent(id int64) error {
	return notFoundError{fmt.Errorf("agent %d not found; list ids with driga agents", id)}
}
