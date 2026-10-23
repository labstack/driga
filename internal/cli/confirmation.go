package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/labstack/driga/internal/actions"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func terminalInput(g *Globals) bool {
	if g == nil {
		return false
	}
	if g.inputTerminal {
		return true
	}
	f, ok := g.Stdin.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(f.Fd())
}
func requireConfirmation(g *Globals, yes bool, prompt string) error {
	if yes {
		return nil
	}
	if !terminalInput(g) {
		return invalidUsage("not applied; pass --yes to apply without asking")
	}
	ok, err := confirmAction(bufio.NewReader(g.Stdin), commandOutput(g), prompt+" [y/N]")
	if err != nil {
		return err
	}
	if !ok {
		return actions.ErrRefused
	}
	return nil
}
func confirmAction(input *bufio.Reader, out io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprint(out, prompt+" "); err != nil {
		return false, err
	}
	line, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.TrimSpace(line)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
func actionEnvironment(g *Globals, yes bool) actions.Environment {
	local := *g
	if local.Stdin != nil {
		local.Stdin = bufio.NewReader(local.Stdin)
		local.inputTerminal = terminalInput(g)
	}
	return actions.Environment{Stdout: commandOutput(g), Machine: g.Machine, Platform: actPlatform, VerifyWindow: verifyWindow,
		Confirm:        func(prompt string) error { return requireConfirmation(&local, yes, prompt) },
		OpenReader:     func(ctx context.Context) (*store.Store, string, func(), error) { return OpenReader(ctx, g) },
		ResolveMachine: func(r view.Reader) (store.Scope, error) { return resolveMachine(g, r) },
	}
}

type AgentsKillCmd struct {
	ID    int64 `arg:"" name:"session-id" help:"Internal session ID."`
	Force bool  `help:"Escalate survivors to SIGKILL (TerminateProcess on Windows); use --yes to skip confirmation."`
	Yes   bool  `short:"y" help:"Skip confirmation; identity and idle checks still apply."`
}

func (c AgentsKillCmd) Run(g *Globals) error {
	if c.ID <= 0 {
		return invalidUsage("session-id must be positive")
	}
	if !c.Yes && !terminalInput(g) {
		return invalidUsage("not applied; pass --yes to apply without asking")
	}
	return actions.KillSession(context.Background(), actionEnvironment(g, c.Yes), c.ID, c.Force)
}

type AgentsJumpCmd struct {
	ID  int64 `arg:"" name:"session-id" help:"Internal session ID."`
	Yes bool  `short:"y" help:"Jump without asking for confirmation."`
}

func (c AgentsJumpCmd) Run(g *Globals) error {
	return actions.JumpSession(context.Background(), actionEnvironment(g, c.Yes), newInboxRunner(), c.ID)
}
