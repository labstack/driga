// Package cli defines driga's kong command tree.
package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

// Globals are flags shared by every command.
type Globals struct {
	JSON     bool          `help:"Print versioned JSON instead of tables."`
	Project  string        `help:"Scope views to one project (path, name, or '.')."`
	Since    string        `help:"Only show items since a duration ago or 'yesterday'."`
	Snapshot bool          `help:"Force snapshot mode (one-shot collect, no history)."`
	NoColor  bool          `name:"no-color" help:"Disable colors."`
	Watch    time.Duration `help:"Re-render every interval (e.g. 2s)." placeholder:"DUR"`

	Version string    `kong:"-"`
	Stdout  io.Writer `kong:"-"`
	Stderr  io.Writer `kong:"-"`
	Stdin   io.Reader `kong:"-"`
}

// Root is the full command tree.
type Root struct {
	Globals

	Summary   SummaryCmd   `cmd:"" default:"1" help:"Show a one-screen summary."`
	Agents    AgentsCmd    `cmd:"" help:"List agents grouped by family."`
	Agent     AgentCmd     `cmd:"" help:"Show session detail."`
	Projects  ProjectsCmd  `cmd:"" help:"List project status."`
	Where     WhereCmd     `cmd:"" help:"Show where a project stands."`
	Inbox     InboxCmd     `cmd:"" help:"List attention and completion events."`
	Conflicts ConflictsCmd `cmd:"" help:"Show current and recent overlaps."`
	Orphans   OrphansCmd   `cmd:"" help:"List orphan process trees."`
	Machine   MachineCmd   `cmd:"" help:"Show machine health."`
	Top       TopCmd       `cmd:"" help:"Open the interactive session viewer."`

	Service    ServiceCmd `cmd:"" help:"Manage the collector service."`
	Doctor     DoctorCmd  `cmd:"" help:"Check setup and collection health."`
	Init       InitCmd    `cmd:"" help:"Guide optional setup."`
	Config     ConfigCmd  `cmd:"" help:"Inspect or edit configuration."`
	Collect    CollectCmd `cmd:"" help:"Run the collector (what launchd runs)."`
	Hook       HookCmd    `cmd:"" hidden:"" help:"Record an agent hook."`
	Hooks      HooksCmd   `cmd:"" help:"Manage agent hooks."`
	VersionCmd VersionCmd `cmd:"" name:"version" help:"Print the driga version."`
}

// VersionCmd prints "driga <version>".
type VersionCmd struct{}

func (VersionCmd) Run(g *Globals) error {
	_, err := fmt.Fprintf(g.Stdout, "driga %s\n", g.Version)
	return err
}

// Main parses args, runs the selected command and returns the exit code.
func Main(args []string, version string) int {
	return run(args, version, os.Stdin, os.Stdout, os.Stderr)
}

func run(args []string, version string, stdin io.Reader, stdout, stderr io.Writer) int {
	var root Root
	root.Version, root.Stdout, root.Stderr, root.Stdin = version, stdout, stderr, stdin
	exit := -1
	parser, err := kong.New(&root,
		kong.Name("driga"),
		kong.Description("Watch every AI coding agent session on this machine."),
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) { exit = code; panic(exitPanic{}) }),
		kong.Bind(&root.Globals),
	)
	if err != nil {
		fmt.Fprintln(stderr, "driga:", err)
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
			fmt.Fprintln(stderr, "driga:", err)
			return 2
		}
		if err := ctx.Run(); err != nil {
			fmt.Fprintln(stderr, "driga:", err)
			if c, ok := err.(interface{ ExitCode() int }); ok {
				return c.ExitCode()
			}
			return 1
		}
		return 0
	}()
	return code
}

type exitPanic struct{}
