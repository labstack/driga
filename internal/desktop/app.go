package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// needsPoll is how often the tray and badge check the data for a change.
const needsPoll = 2 * time.Second

// loginMarker, in the data directory, records the open-at-login setting the
// app last registered.
const loginMarker = "open-at-login"

// Options configure the desktop app.
type Options struct {
	// OpenSource opens the data the app shows, and its closer. Run calls it
	// once it holds the single-instance lock, so a second launch never opens
	// the store.
	OpenSource func() (web.Source, func(), error)
	// Web carries the page handlers the CLI provides: session actions,
	// cleanup, the machine scope and activity. Run sets its Chat, Confirm,
	// OpenExternal and Theme.
	Web web.Options
	// Chat defaults to the running collector's chat broker, dialed on use.
	Chat chat.Client
	// Supervisor runs the collector. Run sets its Notify and Status, and
	// fills Start, ReadOwner, Ready and Executable when they are unset.
	Supervisor *Supervisor
	Config     config.Config
	// ConfigErr is why the settings file could not be read; Config then
	// holds the defaults, and the app says so once it is ready.
	ConfigErr error
	Version   string
}

// shell is the running app: the main window, the tray and the collector.
type shell struct {
	opts    Options
	source  web.Source
	goos    string
	dataDir string
	host    *windowHost
	notes   notifier
	upgrade *upgradeWatch
	// ctx lives as long as the app; the collector runs under it.
	ctx context.Context
	// runCollector runs the supervisor until its context ends.
	runCollector func(context.Context)
	// openService opens the background service the app replaces.
	openService func() (installedService, error)
	// homes returns HOME and the account's home directory.
	homes func() (string, string)
	// loginEnv is the login environment the collector starts with.
	loginEnv *loginEnv
	// relaunchApp quits and starts the app again, and reports false when
	// the quit was cancelled.
	relaunchApp func() bool

	collectMu  sync.Mutex
	finished   bool // The app's final stop ran: no collector starts again.
	stopCancel context.CancelFunc
	stopped    chan struct{}

	mu         sync.Mutex
	cfg        config.Config
	tray       *mygo.Tray
	status     Status
	envErr     error
	serviceErr error
	// shadowed are the DRIGA_* variables the login shell sets otherwise.
	shadowed []string
	// restartPending is set once an upgrade's relaunch was cancelled.
	restartPending bool
	needs          []view.BoardRow
	trayKey        string
	settings       *mygo.Window
}

// Run runs the desktop app until it quits. It must be called on the main
// goroutine. A second launch hands over to the running app and returns nil.
func Run(opts Options) error {
	mygo.App.SetName("Driga")
	mygo.App.SetVersion(opts.Version)
	source, closeSource, first, err := openAfterLock(mygo.App.RequestSingleInstanceLock, opts.OpenSource)
	if !first || err != nil {
		return err
	}
	defer closeSource()
	dataDir := config.DataDir()
	if opts.Chat == nil {
		remote := &collectorChat{dataDir: dataDir}
		defer remote.Close()
		opts.Chat = remote
	}
	s := &shell{opts: opts, source: source, goos: runtime.GOOS, dataDir: dataDir, cfg: opts.Config, relaunchApp: relaunchApp}
	s.host = &windowHost{create: s.newWindow}
	webOpts := opts.Web
	webOpts.Chat = opts.Chat
	webOpts.Confirm = s.confirm
	webOpts.OpenExternal = func(url string) { _ = mygo.Shell.OpenExternal(url) }
	server, err := web.New(source, webOpts)
	if err != nil {
		return err
	}
	exe, err := s.prepareSupervisor(dataDir)
	if err != nil {
		return err
	}
	s.upgrade = newUpgradeWatch(exe, opts.Version)
	s.openService, s.homes = openUserService(exe), accountHomes
	s.runCollector = func(ctx context.Context) {
		if err := opts.Supervisor.Run(ctx); err != nil {
			slog.Warn("collector did not stop cleanly", "event", "collector_stop_failed", "err", err.Error())
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx
	if s.loginEnv != nil {
		go s.loginEnv.refresh(ctx)
	}
	go server.Watch(ctx)
	mygo.Protocol.Handle("mygo", server)
	mygo.Theme.SetSource(themeSource(opts.Config.UI.Theme))
	mygo.App.SetMenu(s.menu(MenuModel(s.goos)))
	mygo.App.OnSecondInstance(func([]string, string) { s.host.Open() })
	mygo.App.OnNotificationClick(func(id string) {
		go func() {
			rows := s.inboxRows()
			mygo.RunOnMain(func() { openNotification(s.host, id, func() []view.InboxRow { return rows }) })
		}()
	})
	mygo.App.OnActivate(func(visible bool) {
		if !visible {
			s.host.Open()
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		if CloseQuits(s.goos, s.trayCreated()) {
			mygo.App.Quit()
		}
	})
	mygo.App.WhenReady(func() {
		if opts.ConfigErr != nil {
			s.reportConfigError(opts.ConfigErr)
		}
		_ = s.applyOpenAtLogin(opts.Config.App.LaunchAtLogin)
		s.applyTray(opts.Config.App.Tray)
		if !startHidden(s.goos, mygo.App.WasOpenedAtLogin(), s.trayCreated()) {
			s.host.Open()
		}
		s.applyDock()
		// The service's collector holds the lock until it stops; the
		// supervisor then starts the app's own.
		s.startCollector(s.replaceService)
		go s.watchNeeds(ctx)
		go s.upgrade.run(ctx, s.relaunch)
	})
	err = mygo.App.Run()
	s.finish()
	return err
}

// openAfterLock takes the single-instance lock and only then opens the
// source. It reports false, opening nothing, when another instance runs.
func openAfterLock(lock func() bool, open func() (web.Source, func(), error)) (web.Source, func(), bool, error) {
	if !lock() {
		return nil, nil, false, nil
	}
	if open == nil {
		return nil, nil, true, errors.New("desktop: no data source")
	}
	source, closeSource, err := open()
	if err != nil {
		return nil, nil, true, err
	}
	return source, closeSource, true, nil
}

// prepareSupervisor fills the supervisor's production defaults and wires its
// reports to the shell. It returns the collector executable.
func (s *shell) prepareSupervisor(dataDir string) (string, error) {
	sup := s.opts.Supervisor
	if sup == nil {
		return "", errors.New("desktop: no collector supervisor")
	}
	if sup.Executable == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		sup.Executable = exe
	}
	if sup.Start == nil {
		s.loginEnv = newLoginEnv(dataDir, LoginEnvironment)
		sup.Start = func(ctx context.Context) (Child, error) {
			// Without the login environment the collector still runs, with
			// the app's own; the status says what is missing.
			if s.upgrade != nil && s.upgrade.pending() {
				return nil, errUpgradePending
			}
			env, err := s.loginEnv.get(ctx)
			shadowed := shadowedKeys(env, os.Getenv)
			s.mu.Lock()
			s.envErr = err
			logShadowed := !slices.Equal(s.shadowed, shadowed)
			s.shadowed = shadowed
			s.mu.Unlock()
			if err != nil {
				slog.Warn("login environment unavailable", "event", "login_env_failed", "err", err.Error())
			}
			if logShadowed && len(shadowed) > 0 {
				slog.Warn("the login shell sets DRIGA_* variables the app does not use", "event", "login_env_shadowed", "keys", strings.Join(shadowed, ","))
			}
			return StartCollector(ctx, sup.Executable, env)
		}
	}
	if sup.ReadOwner == nil {
		sup.ReadOwner = func() (collector.Owner, bool, error) { return collector.ReadOwner(dataDir) }
	}
	if sup.Ready == nil {
		sup.Ready = func(ctx context.Context) error { return WaitChat(ctx, dataDir) }
	}
	sup.Notify = s.notes.post
	sup.Status = s.setStatus
	return sup.Executable, nil
}

// replaceService removes the background service the app replaces, and says
// so once.
func (s *shell) replaceService() {
	if !s.removeService(s.dataDir) {
		s.updateTray()
		return
	}
	// The notice waits for OK; the collector does not.
	go func() {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{Parent: s.mainWindow(), Type: mygo.MessageInfo,
			Message: serviceReplacedMessage, Buttons: []string{"OK"}})
	}()
}

// reportConfigError says the settings could not be read. The collector
// reads the same file, so the tray shows its error until the file is fixed.
func (s *shell) reportConfigError(err error) {
	slog.Warn("settings unreadable; using the defaults", "event", "config_invalid", "err", err.Error())
	go func() {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{Parent: s.mainWindow(), Type: mygo.MessageWarning,
			Message: "Driga could not read its settings and uses the defaults.", Detail: err.Error() + "\n\nFix the file, then quit and reopen Driga.", Buttons: []string{"OK"}})
	}()
}

// removeService removes the background service when the app replaces it, and
// reports whether it did. A failure is kept for the tray, whose status line
// then says how to remove the service by hand.
func (s *shell) removeService(dataDir string) bool {
	removed, err := replaceService(s.goos, dataDir, s.homes, s.openService)
	if errors.Is(err, errIsolatedHome) {
		slog.Info("the app runs in an isolated environment; leaving the collector service alone", "event", "service_replace_isolated")
		return false
	}
	s.mu.Lock()
	s.serviceErr = err
	s.mu.Unlock()
	if err != nil {
		slog.Warn("could not remove the collector service", "event", "service_replace_failed", "err", err.Error())
	}
	if removed {
		slog.Info("removed the collector service; the app collects", "event", "service_replaced")
	}
	return removed
}

// startCollector runs the collector until stopCollector, after first, when
// it is set.
func (s *shell) startCollector(first func()) {
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.collectMu.Lock()
	if s.finished {
		s.collectMu.Unlock()
		cancel()
		return
	}
	s.stopCancel, s.stopped = cancel, done
	s.collectMu.Unlock()
	go func() {
		defer close(done)
		if first != nil {
			first()
		}
		s.runCollector(ctx)
	}()
}

// stopCollector stops the supervisor and waits for its child to exit.
func (s *shell) stopCollector() {
	s.collectMu.Lock()
	cancel, done := s.stopCancel, s.stopped
	s.collectMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// finish is the app's final stop of the collector: none starts after it.
func (s *shell) finish() {
	s.collectMu.Lock()
	s.finished = true
	s.collectMu.Unlock()
	s.stopCollector()
}

// relaunch restarts the app on the upgraded executable, after its collector
// stopped so the new one starts from the new binary, and reports whether the
// quit went ahead. When it is cancelled the app keeps running without a
// collector: one started now would be the upgraded binary's, whose store the
// old app may not read. The tray asks for a restart, and the upgrade watch
// asks again on its next check.
func (s *shell) relaunch() bool {
	slog.Info("executable upgraded; relaunching", "event", "app_relaunch")
	s.stopCollector()
	// The relaunched process starts as the app again, whatever launched this one.
	previous, set := os.LookupEnv("DRIGA_APP")
	_ = os.Setenv("DRIGA_APP", "1")
	if s.relaunchApp() {
		return true
	}
	slog.Info("relaunch cancelled; restart Driga to finish updating", "event", "app_relaunch_cancelled")
	if set {
		_ = os.Setenv("DRIGA_APP", previous)
	} else {
		_ = os.Unsetenv("DRIGA_APP")
	}
	s.mu.Lock()
	s.restartPending = true
	s.mu.Unlock()
	s.updateTray()
	return false
}

// relaunchApp asks the toolkit to relaunch and reports whether the quit went
// ahead. Relaunch quits on the main thread, where a function queued after it
// runs once the quit is over: OnWillQuit has fired by then unless the quit
// was cancelled. Driga's windows close without asking, so no dialog runs the
// queue early.
func relaunchApp() bool {
	var quitting atomic.Bool
	off := mygo.App.OnWillQuit(func(*mygo.QuitEvent) { quitting.Store(true) })
	mygo.App.Relaunch()
	mygo.RunOnMain(off)
	return quitting.Load()
}

func (s *shell) setStatus(st Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	s.updateTray()
}

// trayStatus is the supervisor's status with a missing login environment
// added, which the supervisor does not know about.
func (s *shell) trayStatus() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	switch {
	case s.restartPending:
		st = Status{RestartPending: true}
	case st.Err != nil:
	case s.serviceErr != nil:
		// The error itself is in the log; the line has room for the fix.
		st.Err = errors.New("background service not removed; run driga service uninstall")
	case s.envErr != nil && st.Collecting:
		st.Err = fmt.Errorf("without login shell PATH: %w", s.envErr)
	case len(s.shadowed) > 0 && st.Collecting:
		st.Err = fmt.Errorf("ignoring %s from the login shell; quit Driga and run driga app from it", strings.Join(s.shadowed, ", "))
	}
	return st
}

// watchNeeds keeps the tray and the badge on the current needs.
func (s *shell) watchNeeds(ctx context.Context) {
	last := int64(-1)
	ticker := time.NewTicker(needsPoll)
	defer ticker.Stop()
	for {
		version, err := s.source.DataVersion(ctx)
		if err == nil && version != last {
			if snap, err := s.source.LoadScope(ctx, store.Scope{}); err == nil {
				last = version
				observed := snap.ObservedAt
				if observed.IsZero() {
					observed = time.Now()
				}
				needs := view.Board(snap.BoardData, time.Hour, observed).Needs
				s.mu.Lock()
				changed := len(needs) != len(s.needs)
				s.needs = needs
				s.mu.Unlock()
				if changed {
					mygo.App.SetBadgeCount(len(needs))
				}
				s.updateTray()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// inboxRows reads the inbox for a notification click.
func (s *shell) inboxRows() []view.InboxRow {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := s.source.LoadScope(ctx, store.Scope{})
	if err != nil {
		slog.Warn("inbox unavailable for a notification click", "event", "notification_click_failed", "err", err.Error())
		return nil
	}
	return snap.Inbox.Rows
}

// mainPage is the main window as the window host drives it.
type mainPage struct{ w *mygo.Window }

func (p mainPage) Show() {
	if p.w.IsMinimized() {
		p.w.Restore()
	}
	p.w.Show()
	p.w.Focus()
	mygo.App.Focus()
}

// Eval runs off the main thread, which the page's answer must not hold up.
func (p mainPage) Eval(js string) {
	go func() {
		if page := p.w.Page(); page != nil {
			_, _ = page.Eval(js)
		}
	}()
}

// newWindow opens the main window on the dashboard.
func (s *shell) newWindow(loaded func()) page {
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: "Driga", URL: "/app/", Width: 1280, Height: 820, MinWidth: 720, MinHeight: 480,
		StateKey: "main", BackgroundColor: "light-dark(#eff1f5, #0e0e16)",
	})
	p := mainPage{w}
	w.Page().OnDidFinishLoad(loaded)
	w.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
		if external(e.URL) {
			e.PreventDefault()
			go func() { _ = mygo.Shell.OpenExternal(e.URL) }()
		}
	})
	w.OnClosed(func() {
		s.host.Closed(p)
		s.applyDock()
	})
	// The host is busy creating this window: set the Dock for an open one.
	if s.goos == "darwin" {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	}
	mygo.App.Focus()
	return p
}

// mainWindow is the open main window, or nil.
func (s *shell) mainWindow() *mygo.Window {
	if p, ok := s.host.Current().(mainPage); ok {
		return p.w
	}
	return nil
}

// applyDock shows the Dock icon while it is needed (macOS).
func (s *shell) applyDock() {
	if s.goos != "darwin" {
		return
	}
	policy := mygo.ActivationPolicyAccessory
	if dockRegular(s.host.IsOpen(), s.trayCreated()) {
		policy = mygo.ActivationPolicyRegular
	}
	mygo.App.SetActivationPolicy(policy)
}

// applyOpenAtLogin registers the open-at-login setting when it changed since
// the app last did.
func (s *shell) applyOpenAtLogin(open bool) error {
	err := syncOpenAtLogin(s.dataDir, open, func(open bool) error {
		if mygo.App.OpenAtLogin() == open {
			return nil
		}
		return mygo.App.SetOpenAtLogin(open)
	})
	if err != nil {
		slog.Warn("could not change open at login", "event", "open_at_login_failed", "err", err.Error())
	}
	return err
}

// syncOpenAtLogin calls set on the first launch and when the setting differs
// from the one last set, recorded in dataDir. Other launches leave the login
// item alone, so turning it off in the system's settings sticks.
func syncOpenAtLogin(dataDir string, open bool, set func(bool) error) error {
	path := filepath.Join(dataDir, loginMarker)
	want := strconv.FormatBool(open)
	b, err := os.ReadFile(path)
	if err == nil && string(b) == want {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := set(open); err != nil {
		return err
	}
	if err := secfile.MkdirPrivate(dataDir); err != nil {
		return err
	}
	return secfile.WritePrivate(path, []byte(want))
}

// confirm asks with a native dialog on the main window.
func (s *shell) confirm(_ context.Context, message string) bool {
	text, detail, buttons := confirmDialog(message)
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: s.mainWindow(), Type: mygo.MessageQuestion, Message: text, Detail: detail,
		Buttons: buttons, DefaultButton: 0, CancelButton: 1,
	})
	return err == nil && res.Button == 0
}

// menu builds a toolkit menu whose items run the shell's commands.
func (s *shell) menu(specs []MenuSpec) *mygo.Menu {
	return mygo.NewMenu(s.items(specs))
}

func (s *shell) items(specs []MenuSpec) []*mygo.MenuItem {
	items := make([]*mygo.MenuItem, 0, len(specs))
	for _, spec := range specs {
		if spec.Separator {
			items = append(items, mygo.Separator())
			continue
		}
		item := &mygo.MenuItem{Label: spec.Label, Role: mygo.MenuRole(spec.Role), Accelerator: spec.Accelerator, Disabled: spec.Disabled}
		if spec.Submenu != nil {
			item.Submenu = s.items(spec.Submenu)
		}
		if command := spec.Command; command != "" {
			item.Click = func(*mygo.MenuItem, *mygo.Window) { s.do(command) }
		}
		items = append(items, item)
	}
	return items
}

// do runs a menu command. Commands of the page open the window first.
func (s *shell) do(command string) {
	switch command {
	case CommandAbout:
		s.about()
	case CommandSettings:
		s.openSettings()
	case CommandOpen:
		s.host.Open()
	case CommandFind:
		s.host.Command("find", nil)
	case CommandReload:
		s.host.Command("reload", nil)
	case CommandBack, CommandForward:
		s.host.Command(command, nil)
	case CommandDocs:
		go func() { _ = mygo.Shell.OpenExternal(DocsURL) }()
	default:
		if raw, ok := strings.CutPrefix(command, "session:"); ok {
			if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
				s.host.Command("openSession", id)
			}
		}
	}
}

func (s *shell) about() {
	if s.goos == "darwin" {
		mygo.App.ShowAboutPanel(mygo.AboutPanelOptions{ApplicationName: "Driga", ApplicationVersion: s.opts.Version})
		return
	}
	go func() {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{Parent: s.mainWindow(), Type: mygo.MessageInfo, Title: "About Driga",
			Message: "Driga " + s.opts.Version, Detail: DocsURL, Buttons: []string{"OK"}})
	}()
}

func themeSource(theme string) mygo.ThemeSource {
	switch theme {
	case "light":
		return mygo.ThemeLight
	case "dark":
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}
