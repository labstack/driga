// Package cli defines driga's kong command tree.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/x/term"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/scrub"
)

// Globals carries resolved command options and runtime dependencies.
type Globals struct {
	inputTerminal bool   `kong:"-"`
	Machine       string `kong:"-"`
	// CleanupPlatform injects read-only inspection for embedded callers/tests.
	CleanupPlatform platform.Platform `kong:"-"`
	cachedMachines  bool              `kong:"-"`
	JSON            bool              `kong:"-"`
	Project         string            `kong:"-"`
	Since           string            `kong:"-"`
	Snapshot        bool              `kong:"-"`
	Theme           *string           `kong:"-"`
	NoColor         bool              `kong:"-"`

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
	VersionFlag kong.VersionFlag `name:"version" help:"Print the driga version."`

	Summary    SummaryCmd  `cmd:"" default:"withargs" help:"Show a one-screen summary. Bare driga opens top in a terminal."`
	Flow       FlowCmd     `cmd:"" help:"Show a session family and its state signals."`
	Agents     AgentsCmd   `cmd:"" help:"List agents grouped by family."`
	Projects   ProjectsCmd `cmd:"" help:"List project status."`
	Inbox      InboxCmd    `cmd:"" help:"List attention and completion events."`
	Orphans    OrphansCmd  `cmd:"" help:"List orphan process trees."`
	Machines   MachinesCmd `cmd:"" help:"List known machines and their connection health."`
	Cleanup    CleanupCmd  `cmd:"" help:"Inspect and explicitly clean local agent disk candidates."`
	Top        TopCmd      `cmd:"" help:"Open the interactive session viewer."`
	App        AppCmd      `cmd:"" help:"Open the Driga desktop app, or focus it."`
	Ask        AskCmd      `cmd:"" help:"Ask a one-shot Chat question through Claude and driga's read-only tools."`
	MCP        MCPCmd      `cmd:"" name:"mcp" help:"Serve collected views and editable dashboards over MCP stdio."`
	Doctor     DoctorCmd   `cmd:"" help:"Check setup and collection health."`
	Init       InitCmd     `cmd:"" help:"Guide optional setup."`
	Config     ConfigCmd   `cmd:"" help:"Inspect or edit configuration."`
	Service    ServiceCmd  `cmd:"" help:"Run or manage the Driga service (drigad)."`
	Hooks      HooksCmd    `cmd:"" help:"Manage agent hooks."`
	Fleet      FleetCmd    `cmd:"" help:"Inspect fleet identity, serving status and viewers."`
	Hook       HookCmd     `cmd:"" hidden:"" help:"Record an agent hook."`
	Feed       FeedCmd     `cmd:"" hidden:"" help:"Inspect the local fleet feed."`
	Dev        DevCmd      `cmd:"" hidden:"" help:"Inspect unsupported agent formats."`
	VersionCmd VersionCmd  `cmd:"" name:"version" help:"Print the driga version."`
}

// VersionCmd prints the version and release build metadata.
type VersionCmd struct{ JSONOptions }

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
		if len(args) > 0 && args[0] == "hook" {
			return 0
		}
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
		_ = (HookCmd{Adapter: args[1], Event: args[2]}).Run(&Globals{Stdin: stdin, Stdout: stdout, Stderr: stderr})
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
	parser, err := NewParser(&root,
		kong.Vars{"version": "driga " + version},
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) { exit = code; panic(exitPanic{}) }),
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
		run := ctx.Run
		if ctx.Command() == "summary" && !slices.Contains(args, "summary") && !root.JSON && interactive(stdin, stdout) {
			// Bare driga in a terminal is the viewer; pipes and --json keep the summary.
			run = func(...any) error { return TopCmd{Tab: "flow"}.Run(&root.Globals) }
		}
		if err := run(); err != nil {
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

// NewParser builds the kong parser for the full command tree. The CLI and the
// documentation generator (cmd/driga-docgen) share it, so `driga --help` and
// the published command reference come from one model.
func NewParser(root *Root, options ...kong.Option) (*kong.Kong, error) {
	return kong.New(root, append([]kong.Option{
		kong.Name("driga"),
		kong.Description("Watch AI coding agent sessions across your machines and projects."),
		kong.Bind(&root.Globals),
	}, options...)...)
}

type exitPanic struct{}

func interactive(in io.Reader, out io.Writer) bool {
	i, iok := in.(interface{ Fd() uintptr })
	o, ook := out.(interface{ Fd() uintptr })
	return iok && ook && term.IsTerminal(i.Fd()) && term.IsTerminal(o.Fd())
}

// reportedError preserves a structured partial mutation result already written.
type reportedError struct{ error }

// Reject unsupported formats before starting interactive commands or mutations.
func supportsJSON(command string) bool {
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "flow", "summary", "ask", "doctor", "version", "dev", "feed":
		return true
	case "agents":
		return len(words) < 2 || words[1] == "list" || words[1] == "show"
	case "projects":
		return true
	case "machines":
		return len(words) < 2 || words[1] == "list" || words[1] == "show"
	case "cleanup":
		return true
	case "inbox", "orphans":
		return len(words) < 2 || words[1] == "list"
	case "config":
		return len(words) > 1 && words[1] == "path"
	case "fleet":
		if len(words) < 2 {
			return false
		}
		if words[1] == "id" || words[1] == "status" {
			return true
		}
		return len(words) > 2 && (words[1] == "peers" && (words[2] == "list" || words[2] == "add") || words[1] == "viewers" && words[2] == "list")
	}
	return false
}

type usageError struct{ error }

func (usageError) ExitCode() int                    { return 2 }
func invalidUsage(format string, args ...any) error { return usageError{fmt.Errorf(format, args...)} }

type notFoundError struct{ error }

func missingAgent(id int64) error {
	return notFoundError{fmt.Errorf("agent %d not found; list ids with driga agents", id)}
}
