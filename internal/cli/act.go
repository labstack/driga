package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/labstack/driga/internal/actions"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/view"
)

// OrphansKillCmd terminates explicitly confirmed orphan trees.
type OrphansKillCmd struct {
	OrphanActionOptions
	Yes   bool   `short:"y" help:"Skip confirmation; identity and idle checks still apply."`
	ID    string `arg:"" name:"orphan-id" optional:"" help:"Orphan candidate ID."`
	All   bool   `help:"Select all local orphan candidates; verify and confirm each unless --yes"`
	Force bool   `help:"Escalate survivors to SIGKILL (TerminateProcess on Windows); confirmation is still required unless --yes."`
}

// InboxAckCmd records explicit acknowledgments through the collector's spool.
type InboxAckCmd struct {
	MachineOptions
	ID  string `arg:"" name:"item-id" optional:"" help:"Inbox item ID."`
	All bool   `help:"Acknowledge every unacknowledged item."`
}

func actionSelection(id string, all bool) error {
	if id == "" && !all || id != "" && all {
		return invalidUsage("provide an id or --all")
	}
	return nil
}

// Run displays and confirms candidates, then revalidates each tree before kill.
// actPlatform and verifyWindow are replaced in tests.
var (
	actPlatform  = platform.New
	verifyWindow = 3 * time.Second
)

func (c OrphansKillCmd) Run(g *Globals) error { return c.run(context.Background(), g) }
func (c OrphansKillCmd) run(ctx context.Context, g *Globals) error {
	if !c.Yes && !terminalInput(g) {
		return invalidUsage("not applied; pass --yes to apply without asking")
	}
	if err := actionSelection(c.ID, c.All); err != nil {
		return err
	}
	st, _, closeFn, err := OpenReader(ctx, g)
	if err != nil {
		return err
	}
	defer closeFn()
	scope, err := resolveMachine(g, st)
	if err != nil {
		return err
	}
	orphans, err := st.OrphansScoped(scope)
	if err != nil {
		return err
	}
	var candidates []model.Orphan
	for _, o := range orphans {
		if c.All || o.ID == c.ID {
			candidates = append(candidates, o)
		}
	}
	if !c.All && len(candidates) == 0 {
		return fmt.Errorf("unknown orphan id %q", c.ID)
	}
	// Explicit remote selections refuse before verification or platform calls.
	// Bulk selection keeps only local candidates.
	machines, err := st.Machines()
	if err != nil {
		return err
	}
	local := candidates[:0]
	skipped := 0
	for _, o := range candidates {
		if o.MachineID != st.MachineID() {
			if c.All {
				skipped++
				continue
			}
			name := o.MachineID
			for _, m := range machines {
				if m.MachineID == o.MachineID {
					name = view.MachineName(m)
				}
			}
			return fmt.Errorf("orphan is on %s; run driga orphans kill there", name)
		}
		local = append(local, o)
	}
	candidates = local
	if skipped > 0 {
		if _, err := fmt.Fprintf(g.Stdout, "skipped %d orphans on other machines\n", skipped); err != nil {
			return err
		}
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	procs, err := st.Procs()
	if err != nil {
		return err
	}
	return c.killCandidates(ctx, g, cfg, procs, candidates)
}

func (c OrphansKillCmd) killCandidates(ctx context.Context, g *Globals, cfg config.Config, procs []model.Proc, candidates []model.Orphan) error {
	return (actions.KillOptions{All: c.All, Force: c.Force}).Kill(ctx, actionEnvironment(g, c.Yes), cfg, procs, candidates)
}
func (c InboxAckCmd) Run(g *Globals) error {
	return (actions.AckOptions{ID: c.ID, All: c.All}).Ack(actionEnvironment(g, true))
}
