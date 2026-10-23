package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
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
	JSONOptions
	AfterSeq int64 `name:"after-seq" help:"Print rows after this non-negative sequence (default retained beginning)."`
	Follow   bool  `help:"Keep printing new rows until interrupted." short:"f"`
}

// Run prints the retained feed and optionally follows subsequent commits.
func (c FeedTailCmd) Run(g *Globals) error {
	if g.Since != "" {
		return invalidUsage("feed tail uses --after-seq, not --since")
	}
	seq := c.AfterSeq
	if seq < 0 {
		return invalidUsage("feed tail --after-seq must be a nonnegative sequence")
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
	ctx, stop := notifyContext(context.Background())
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
