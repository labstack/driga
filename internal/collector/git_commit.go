package collector

import (
	"context"
	"log/slog"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/store"
)

// Applying a completed inventory does not poll adapters or process sources.
// The daemon remains the only writer and acknowledges results only on commit.
func applyReadyGit(ctx context.Context, d Deps, st *store.Store) (bool, error) {
	w := d.gitWork
	w.mu.Lock()
	job := w.ready
	w.mu.Unlock()
	if job == nil {
		return false, nil
	}
	d.State.mu.Lock()
	defer d.State.mu.Unlock()
	enabled, err := collectFeedEnabled(d, st.DataDir())
	if err != nil {
		return false, err
	}
	revision := st.Revision()
	r := PassReport{SourceErrors: make(map[string]string)}
	err = st.Tx(ctx, func(tx *store.Tx) error {
		if _, err := configurePassFeed(d, tx, enabled); err != nil {
			return err
		}
		share := feed.EffectiveShare(feed.ShareText(d.Cfg.Fleet.ShareText), d.Cfg.CaptureText)
		var before map[string]passFeedRow
		if enabled {
			var err error
			before, err = passFeedRows(tx, job.at, share)
			if err != nil {
				return err
			}
		}
		if err := writeGit(tx, job.results, job.err, func(name string, err error) error {
			if err != nil {
				r.SourceErrors[name] = scrub.Scrub(err.Error())
				return tx.SetSourceError(name, r.SourceErrors[name], job.at)
			}
			return tx.SetSourceOK(name, job.at)
		}, &r); err != nil {
			return err
		}
		if enabled {
			after, err := passFeedRows(tx, job.at, share)
			if err != nil {
				return err
			}
			return appendPassChanges(tx, before, after, job.at)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if d.State.sessionRevision == revision && st.Revision() == revision+1 {
		d.State.sessionRevision = revision + 1
	}
	if job.full && r.SourceErrors["git"] == "" {
		d.State.gitFullAt = job.at
	}
	w.ack(job)
	for name, message := range r.SourceErrors {
		slog.Warn("collector source failed", "event", "collector_source_failed", "source", name, "err", message)
	}
	return job.full, nil
}
