package cli

import (
	"github.com/labstack/driga/internal/actions"

	"github.com/labstack/driga/internal/attention"
)

// JumpCmd follows a local stored inbox destination without acknowledging it.
type JumpCmd struct {
	ID string `arg:"" name:"item-id" help:"Inbox item ID."`
}

func (c JumpCmd) Run(g *Globals) error {
	return c.run(g, newInboxRunner(), "")
}

func (c JumpCmd) run(g *Globals, runner attention.Runner, goos string) error {
	return actions.InboxJump(actionEnvironment(g, true), c.ID, runner, goos)
}
