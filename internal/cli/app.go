package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/desktop"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/service"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// AppCmd opens the desktop app, or focuses the running one.
type AppCmd struct{}

var (
	// openedAtLogin and runDesktop are the desktop package's, replaced in tests.
	openedAtLogin = desktop.OpenedAtLogin
	runDesktop    = desktop.Run
	// openAppViewer opens the app's data; tests count its calls.
	openAppViewer = openViewer
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
	restore := appLogging()
	defer restore()
	// Opened from Finder or at login, an error here would reach no one: the
	// app starts with the default settings and says what is wrong instead.
	cfg, err := config.Load(config.Path())
	var configErr error
	if err != nil {
		cfg, configErr = config.Default(), fmt.Errorf("read %s: %w", config.Path(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.Machine = "all"
	local := *g
	local.Machine = "local"
	return runDesktop(desktop.Options{
		// The app opens the viewer once it is the only instance.
		OpenSource: func() (web.Source, func(), error) {
			source, _, closeSource, err := openAppViewer(ctx, g, cfg, view.Filter{})
			return source, closeSource, err
		},
		// The page's buttons run Chat's local actions. Acknowledgements
		// reach other machines' items too, as the TUI's do.
		Web: web.Options{Action: func(ctx context.Context, name string, args json.RawMessage) error {
			actionGlobals := local
			if name == "ack_inbox" {
				actionGlobals.Machine = g.Machine
			}
			actionGlobals.Stdout = io.Discard
			return runChatAction(ctx, &actionGlobals, name, args)
		}, Machine: g.Machine, Activity: pageActivity, Dashboards: dashboard.New(config.DataDir()), CleanupList: func(ctx context.Context) ([]cleanup.Candidate, error) {
			envelope, err := cleanupRead(ctx, &local, "", false, true)
			if err != nil {
				return nil, err
			}
			return envelope.Data.([]cleanup.Candidate), nil
		}},
		Supervisor: &desktop.Supervisor{Executable: collectorExecutable},
		Config:     cfg,
		ConfigErr:  configErr,
		Version:    g.Version,
	})
}

// appLogging writes the app's log, and its collector's output, to the
// collector log, which driga service logs shows. A launch from Finder or at
// login has nowhere else to write; stderr keeps a terminal run readable.
func appLogging() func() {
	sink := io.MultiWriter(&logfile.Writer{Path: service.LogPath()}, os.Stderr)
	previous := slog.Default()
	slog.SetDefault(slog.New(logfile.TextHandler(sink)).With("process", "app"))
	return func() { slog.SetDefault(previous) }
}

// pageActivity keeps the collector's viewer heartbeat fresh while the page is used.
func pageActivity(active bool) {
	if active {
		_ = touchHeartbeat()
	}
}
