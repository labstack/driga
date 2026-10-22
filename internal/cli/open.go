package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/filelock"
	"github.com/labstack/driga/internal/secfile"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
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
			// A recent pass alone is not enough: after `collect --once` or a
			// stopped service the data only ages, so it must not read as daemon.
			if metaErr == nil && parseErr == nil && time.Since(last) <= 10*time.Minute && collector.Running(config.DataDir()) {
				return st, "daemon", nil
			}
			_ = st.Close()
		} else if (errors.Is(err, store.ErrSchemaPreRelease) || errors.Is(err, store.ErrSchemaNewer)) && g != nil && g.Stderr != nil {
			// Explain incompatible stored data before falling back to a snapshot.
			fmt.Fprintf(g.Stderr, "driga: %v; showing a snapshot\n", err)
		}
	}
	return nil, "snapshot", nil
}

func snapshotHint(w io.Writer, now time.Time) {
	dir := config.DataDir()
	path := filepath.Join(dir, "hint")
	date := now.Format("2006-01-02")
	if err := secfile.MkdirPrivate(dir); err != nil {
		return
	}
	if err := secfile.Protect(dir); err != nil {
		return
	}
	f, err := secfile.OpenFile(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return
	}
	defer f.Close()
	// Serialize simultaneous snapshot readers without taking the collector lock.
	if err := filelock.Lock(f, false); err != nil {
		return
	}
	defer filelock.Unlock(f)
	data, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil || strings.TrimSpace(string(data)) == date {
		return
	}
	err = secfile.ProtectFile(f)
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

// openViewer shares daemon selection, scoped views and disposable collection.
func openViewer(ctx context.Context, g *Globals, cfg config.Config, f view.Filter) (viewer.ScopedSource, string, func(), error) {
	since, err := parseSince(g.Since, time.Now())
	if err != nil {
		return nil, "", nil, err
	}
	f.Project, f.Since = g.Project, since
	f.CaptureText = cfg.CaptureText
	resolve := func(r view.Reader, f view.Filter) (view.Filter, error) {
		resolved, err := resolveViewFilter(g, r, f, g.Project)
		if g.Project == "" {
			resolved.Scope = store.Scope{}
		}
		return resolved, err
	}

	running := func() bool { return !g.Snapshot && collector.Running(config.DataDir()) }
	factory := func(ctx context.Context, daemon bool) (viewer.ScopedSource, func(), error) {
		if daemon {
			st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
			if err != nil {
				return nil, nil, err
			}
			source := viewer.NewStoreSource(st, config.DataDir(), f)
			source.ResolveFilter = resolve
			return source, func() { _ = st.Close() }, nil
		}
		deps, err := NewDeps(cfg)
		if err != nil {
			return nil, nil, err
		}
		source, err := viewer.NewLiveSnapshotSource(deps, config.DataDir(), f)
		if err != nil {
			return nil, nil, err
		}
		source.ResolveFilter = resolve
		source.Start(ctx)
		return source, func() { _ = source.Close() }, nil
	}
	source, err := viewer.NewSwitchingSource(ctx, running, factory)
	if err != nil {
		return nil, "", nil, err
	}
	mode := "snapshot"
	if running() {
		mode = "daemon"
	}
	return source, mode, func() { _ = source.Close() }, nil
}
