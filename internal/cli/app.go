package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/dashboard"
	"github.com/labstack/driga/internal/desktop"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/service"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// AppCmd opens the desktop app, or focuses the running one.
type AppCmd struct {
	CPUProfile        string        `name:"cpu-profile" type:"path" help:"Write a CPU profile of the app to this file."`
	ProfileFor        time.Duration `name:"profile-for" default:"2m" help:"How long --cpu-profile records before it stops."`
	NotificationStdin bool          `hidden:"" help:"Read a notification target from standard input."`
	Notification      string        `hidden:"" help:"Desktop notification event target."`
}

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
	// serviceExecutable is the CLI executable the app installs the Driga
	// service from; empty means this one.
	serviceExecutable string
)

// AppLaunch reports whether this process runs as the desktop app. It reads
// DRIGA_APP and unsets it, so neither config loading, which rejects unknown
// DRIGA_* names, nor the processes the app starts see it. Call it first, on
// the main goroutine, with the command-line arguments.
func AppLaunch(args []string) bool {
	login := len(args) == 0 && os.Getenv("DRIGA_APP") != "1" && openedAtLogin()
	app := appLaunch(os.Getenv, login, args)
	systemLaunch = app && (len(args) == 0 || os.Getenv("DRIGA_APP") == "1")
	_ = os.Unsetenv("DRIGA_APP")
	return app
}

// appLaunch decides app mode: `driga app`, or a bare launch by the system.
func appLaunch(env func(string) string, openedAtLogin bool, args []string) bool {
	if len(args) > 0 && args[0] == "app" {
		if len(args) == 1 {
			return true
		}
		flag, _, _ := strings.Cut(args[1], "=")
		switch flag {
		case "--notification", "--notification-stdin", "--cpu-profile", "--profile-for":
			return true
		}
	}

	return len(args) == 0 && (env("DRIGA_APP") == "1" || openedAtLogin)
}

// AppMain runs the desktop app from the Windows app executable, Driga.exe,
// whose service is installed from the CLI executable at cli.
func AppMain(cli, version string, metadata ...string) int {
	_ = os.Unsetenv("DRIGA_APP")
	systemLaunch, serviceExecutable = true, cli
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "app" {
		args = args[1:]
	}
	return Main(append([]string{"app"}, args...), version, metadata...)
}

// Run must run on the main goroutine: the app's toolkit owns the main thread.
func (c AppCmd) Run(g *Globals) error {
	var target *model.NotificationTarget
	var args []string
	if c.NotificationStdin {
		input := io.Reader(os.Stdin)
		if g.Stdin != nil {
			input = g.Stdin
		}
		raw, err := io.ReadAll(io.LimitReader(input, 4097))
		if err != nil || len(raw) > 4096 {
			return fmt.Errorf("notification input unavailable")
		}
		var parsed model.NotificationTarget
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("notification target: %w", err)
		}
		c.Notification = string(raw)
		// mygo forwards these Go values over its single-instance IPC channel;
		// changing os.Args does not change the kernel's process argument vector.
		original := os.Args
		os.Args = append([]string{original[0]}, "app", "--notification", c.Notification)
		defer func() { os.Args = original }()
	}

	if c.Notification != "" {
		target = new(model.NotificationTarget)
		if err := json.Unmarshal([]byte(c.Notification), target); err != nil {
			return fmt.Errorf("notification target: %w", err)
		}
		args = []string{"app", "--notification", c.Notification}
	}
	if c.CPUProfile != "" {
		if len(args) == 0 {
			args = []string{"app"}
		}
		args = append(args, "--cpu-profile", c.CPUProfile, "--profile-for", c.ProfileFor.String())
	}
	if handed, err := handOffApp(systemLaunch || c.NotificationStdin, args...); handed || err != nil {
		return err
	}
	if c.CPUProfile != "" {
		stopProfile, err := startCPUProfile(c.CPUProfile, c.ProfileFor)
		if err != nil {
			return err
		}
		defer stopProfile()
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
		Notification: target,
		// The app opens the viewer once it is the only instance.
		OpenSource: func() (web.Source, func(), error) {
			source, _, closeSource, err := openAppViewer(ctx, g, cfg, view.Filter{})
			return source, closeSource, err
		},
		Web:               pageOptions(g, &local),
		ServiceExecutable: serviceExecutable,
		Config:            cfg,
		ConfigErr:         configErr,
		Version:           g.Version,
	})
}

// pageOptions are the page's handlers, the same in the app and in driga dev
// serve. The page's buttons run Chat's local actions on this machine;
// acknowledgements reach other machines' items too, as the TUI's do.
func pageOptions(g, local *Globals) web.Options {
	return web.Options{Action: func(ctx context.Context, name string, args json.RawMessage) error {
		actionGlobals := *local
		if name == "ack_inbox" {
			actionGlobals.Machine = g.Machine
		}
		actionGlobals.Stdout = io.Discard
		return runChatAction(ctx, &actionGlobals, name, args)
	}, Activity: pageActivity, Dashboards: dashboard.New(config.DataDir()), CleanupList: func(ctx context.Context) ([]cleanup.Candidate, error) {
		envelope, err := cleanupRead(ctx, local, "", false, true)
		if err != nil {
			return nil, err
		}
		return envelope.Data.([]cleanup.Candidate), nil
	}}
}

// appLogging writes the app's log to the Driga service's log, which driga
// service logs shows. A launch from Finder or at
// login has nowhere else to write; stderr keeps a terminal run readable.
func appLogging() func() {
	sink := io.MultiWriter(&logfile.Writer{Path: service.LogPath()}, os.Stderr)
	previous := slog.Default()
	slog.SetDefault(slog.New(logfile.TextHandler(sink)).With("process", "app"))
	return func() { slog.SetDefault(previous) }
}

// pageActivity keeps the service's viewer heartbeat fresh while the page is used.
func pageActivity(active bool) {
	if active {
		_ = touchHeartbeat()
	}
}
