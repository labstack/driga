package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
)

// FeedCmd contains hidden fleet debugging commands.
type FeedCmd struct {
	Tail FeedTailCmd `cmd:"" help:"Print committed feed rows as JSON lines."`
}

// FeedTailCmd reads the local feed without collecting or acknowledging anything.
type FeedTailCmd struct {
	Follow bool `help:"Keep printing new rows until interrupted." short:"f"`
}

// Run prints the retained feed and optionally follows subsequent commits.
func (c FeedTailCmd) Run(g *Globals) error {
	// --since is inherited from Globals; Kong disallows redeclaring it here.
	var seq int64
	if g != nil && g.Since != "" {
		var err error
		seq, err = strconv.ParseInt(g.Since, 10, 64)
		if err != nil || seq < 0 {
			return fmt.Errorf("feed tail --since must be a nonnegative sequence")
		}
	}
	st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
	if err != nil {
		return err
	}
	defer st.Close()
	epoch, min, _, err := st.FeedBounds()
	if err != nil {
		return err
	}
	// With no explicit cursor, tail starts at the retained beginning.
	if seq == 0 {
		seq = min - 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	encoder := json.NewEncoder(commandOutput(g))
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		rows, err := st.FeedSince(epoch, seq, 1000)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return err
			}
			seq = row.Seq
		}
		if len(rows) == 1000 {
			continue
		}
		if !c.Follow {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
