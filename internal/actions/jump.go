package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
)

func InboxJump(g Environment, id string, runner attention.Runner, goos string) error {
	st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
	if err != nil {
		if _, statErr := os.Stat(filepath.Join(config.DataDir(), "driga.db")); os.IsNotExist(statErr) {
			return fmt.Errorf("inbox item %q no longer exists", id)
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
		if i.ID != id {
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
		opts := attention.JumpOptions{DrigaPath: bin, Tmux: os.Getenv("TMUX"), Terminal: cfg.Inbox.JumpTerminal, OS: goos}
		_, err = attention.JumpCommands(i.Jump, opts)
		if err != nil {
			return fmt.Errorf("inbox item %q has no jump", id)
		}
		if command := attention.JumpCommand(i.Jump); command != "" {
			if _, err := fmt.Fprintln(g.Stdout, command); err != nil {
				return err
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		notice, err := attention.RunJump(ctx, runner, i.Jump, opts)
		if err != nil {
			return fmt.Errorf("jump to inbox item %q: %w", id, err)
		}
		if notice != "" {
			_, err = fmt.Fprintln(g.Stdout, notice)
		}
		return err
	}
	return fmt.Errorf("inbox item %q no longer exists", id)
}
