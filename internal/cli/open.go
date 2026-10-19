package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
)

// OpenReader opens fresh daemon data read-only, or collects a disposable snapshot.
func OpenReader(ctx context.Context, g *Globals) (r *store.Store, mode string, closeFn func(), err error) {
	if ctx.Err() != nil {
		return nil, "", nil, ctx.Err()
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return nil, "", nil, err
	}
	st, mode, err := decideMode(ctx, g)
	if err != nil {
		return nil, "", nil, err
	}
	if st != nil {
		return st, mode, func() { _ = st.Close() }, nil
	}
	deps, err := NewDeps(cfg)
	if err != nil {
		return nil, "", nil, err
	}
	st, err = collector.Snapshot(ctx, deps)
	if err != nil {
		return nil, "", nil, err
	}
	out := io.Discard
	if g != nil && g.Stderr != nil {
		out = g.Stderr
	}
	if !cfg.NoServiceHint {
		snapshotHint(out, time.Now())
	}
	return st, "snapshot", func() { _ = st.Close() }, nil
}

// decideMode shares the read-only freshness decision with the live TUI. A nil
// store asks the caller to collect its own disposable snapshot.
func decideMode(ctx context.Context, g *Globals) (*store.Store, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if g == nil || !g.Snapshot {
		if st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly); err == nil {
			if g != nil && g.cachedMachines {
				return st, "daemon", nil
			}
			raw, metaErr := st.Meta("last_pass_at")
			last, parseErr := time.Parse(time.RFC3339Nano, raw)
			if metaErr == nil && parseErr == nil && time.Since(last) <= 10*time.Minute {
				return st, "daemon", nil
			}
			_ = st.Close()
		}
	}
	return nil, "snapshot", nil
}

func snapshotHint(w io.Writer, now time.Time) {
	dir := config.DataDir()
	path := filepath.Join(dir, "hint")
	date := now.Format("2006-01-02")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	// Serialize simultaneous snapshot readers without taking the collector lock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	data, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil || strings.TrimSpace(string(data)) == date {
		return
	}
	err = f.Chmod(0600)
	if err == nil {
		err = f.Truncate(0)
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err == nil {
		_, err = io.WriteString(f, date+"\n")
	}
	if err != nil {
		return
	}
	fmt.Fprintln(w, "snapshot · no history — run 'driga service install' for inbox & history")
}
