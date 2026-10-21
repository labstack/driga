package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/store"
)

// JumpCmd follows a local stored inbox destination without acknowledging it.
type JumpCmd struct {
	ID string `arg:"" help:"Inbox item ID."`
}

func (c JumpCmd) Run(g *Globals) error { return c.run(g, newInboxRunner(), "") }

func (c JumpCmd) run(g *Globals, runner inbox.Runner, goos string) error {
	st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
	if err != nil {
		if _, statErr := os.Stat(filepath.Join(config.DataDir(), "driga.db")); os.IsNotExist(statErr) {
			return fmt.Errorf("inbox item %q no longer exists", c.ID)
		}
		return fmt.Errorf("read inbox: %w", err)
	}
	defer st.Close()
	// A notification names a local item. Never execute a cached remote jump, or
	// collect a new snapshot whose item IDs may differ from the original alert.
	items, err := st.Inbox(time.Time{}, true)
	if err != nil {
		return err
	}
	for _, i := range items {
		if i.ID != c.ID {
			continue
		}
		cfg, err := config.Load(config.Path())
		if err != nil {
			return err
		}
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		opts := inbox.JumpOptions{DrigaPath: bin, Tmux: os.Getenv("TMUX"), Terminal: cfg.Inbox.JumpTerminal, OS: goos}
		_, err = inbox.JumpCommands(i.Jump, opts)
		if err != nil {
			return fmt.Errorf("inbox item %q has no jump", c.ID)
		}
		if command := inbox.JumpCommand(i.Jump); command != "" {
			if _, err := fmt.Fprintln(commandOutput(g), command); err != nil {
				return err
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		notice, err := inbox.RunJump(ctx, runner, i.Jump, opts)
		if err != nil {
			return fmt.Errorf("jump to inbox item %q: %w", c.ID, err)
		}
		if notice != "" {
			_, err = fmt.Fprintln(commandOutput(g), notice)
		}
		return err
	}
	return fmt.Errorf("inbox item %q no longer exists", c.ID)
}
