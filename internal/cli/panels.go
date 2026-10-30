package cli

import (
	"context"
	"io"
	"slices"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

var panelCleanupSizes cleanup.SizeCache

// runPanels runs a dashboard over one snapshot of every machine, the same
// read the app's views use. Disk candidates are inspected only when a panel
// shows them, since that walks the local worktrees.
func runPanels(ctx context.Context, g *Globals, req panel.RunRequest) (panel.Fragment, error) {
	if err := panel.Validate(req.Dashboard); err != nil {
		return panel.Fragment{}, err
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return panel.Fragment{}, err
	}
	local := *g
	local.Stdout, local.JSON = io.Discard, false
	source, _, closeSource, err := openViewer(ctx, &local, cfg, view.Filter{})
	if err != nil {
		return panel.Fragment{}, err
	}
	defer closeSource()
	snap, err := source.LoadScope(ctx, store.Scope{}, false)
	if err != nil {
		return panel.Fragment{}, err
	}
	if slices.ContainsFunc(req.Dashboard.Panels, func(p panel.Panel) bool { return p.Source == "cleanup" }) {
		local.Machine = "local"
		if envelope, err := cleanupRead(ctx, &local, "", false, true); err == nil {
			if candidates, ok := envelope.Data.([]cleanup.Candidate); ok {
				snap.Cleanup = panelCleanupSizes.Snapshot(candidates)
			}
		}
	}
	return panel.Run(snap, req)
}
