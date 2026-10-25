package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/desktop"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// AppCmd opens the desktop app, or focuses the running one.
type AppCmd struct{}

var (
	// openedAtLogin and runDesktop are the desktop package's, replaced in tests.
	openedAtLogin = desktop.OpenedAtLogin
	runDesktop    = desktop.Run
	// systemLaunch records that the system started this process as the app:
	// Finder, the Dock or Launch Services (DRIGA_APP=1), the login item, or
	// the Windows app executable.
	systemLaunch bool
	// collectorExecutable is the CLI executable the app starts as its
	// collector; empty means this one.
	collectorExecutable string
)

// AppLaunch reports whether this process runs as the desktop app. It reads
// DRIGA_APP and unsets it, so neither config loading, which rejects unknown
// DRIGA_* names, nor the processes the app starts see it. Call it first, on
// the main goroutine, with the command-line arguments.
func AppLaunch(args []string) bool {
	login := len(args) == 0 && os.Getenv("DRIGA_APP") != "1" && openedAtLogin()
	app := appLaunch(os.Getenv, login, args)
	systemLaunch = app && len(args) == 0
	_ = os.Unsetenv("DRIGA_APP")
	return app
}

// appLaunch decides app mode: `driga app`, or a bare launch by the system.
func appLaunch(env func(string) string, openedAtLogin bool, args []string) bool {
	if len(args) == 1 && args[0] == "app" {
		return true
	}
	return len(args) == 0 && (env("DRIGA_APP") == "1" || openedAtLogin)
}

// AppMain runs the desktop app from the Windows app executable, Driga.exe,
// whose collector is the CLI executable at collector.
func AppMain(collector, version string, metadata ...string) int {
	_ = os.Unsetenv("DRIGA_APP")
	systemLaunch, collectorExecutable = true, collector
	return Main([]string{"app"}, version, metadata...)
}

// Run must run on the main goroutine: the app's toolkit owns the main thread.
func (AppCmd) Run(g *Globals) error {
	if handed, err := handOffApp(systemLaunch); handed || err != nil {
		return err
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.Machine = "all"
	source, _, closeSource, err := openViewer(ctx, g, cfg, view.Filter{})
	if err != nil {
		return err
	}
	defer closeSource()
	local := *g
	local.Machine = "local"
	return runDesktop(desktop.Options{
		Source: source,
		Web: web.Options{SessionAction: func(ctx context.Context, action string, id int64) error {
			raw, _ := json.Marshal(map[string]any{"session_id": id})
			actionGlobals := local
			actionGlobals.Stdout = io.Discard
			return runChatAction(ctx, &actionGlobals, action, raw)
		}, Machine: g.Machine, Activity: pageActivity, CleanupList: func(ctx context.Context) ([]cleanup.Candidate, error) {
			envelope, err := cleanupRead(ctx, &local, "", false, true)
			if err != nil {
				return nil, err
			}
			return envelope.Data.([]cleanup.Candidate), nil
		}},
		Supervisor: &desktop.Supervisor{Executable: collectorExecutable},
		Config:     cfg,
		Version:    g.Version,
	})
}

// pageActivity keeps the collector's viewer heartbeat fresh while the page is used.
func pageActivity(active bool) {
	if active {
		_ = touchHeartbeat()
	}
}
