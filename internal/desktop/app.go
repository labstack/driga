package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/web"
)

// needsPoll is how often the tray and badge check the data for a change.
const needsPoll = 2 * time.Second

// Options configure the desktop app.
type Options struct {
	Source web.Source
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
	Version    string
}

// shell is the running app: the main window, the tray and the collector.
type shell struct {
	opts    Options
	goos    string
	host    *windowHost
	notes   notifier
	upgrade *upgradeWatch

	stopOnce   sync.Once
	stopCancel context.CancelFunc
	stopped    chan struct{}

	mu       sync.Mutex
	cfg      config.Config
	tray     *mygo.Tray
	status   Status
	envErr   error
	needs    []view.BoardRow
	trayKey  string
	settings *mygo.Window
}

// Run runs the desktop app until it quits. It must be called on the main
// goroutine. A second launch hands over to the running app and returns nil.
func Run(opts Options) error {
	mygo.App.SetName("Driga")
	mygo.App.SetVersion(opts.Version)
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	dataDir := config.DataDir()
	if opts.Chat == nil {
		remote := &collectorChat{dataDir: dataDir}
		defer remote.Close()
		opts.Chat = remote
	}
	s := &shell{opts: opts, goos: runtime.GOOS, cfg: opts.Config, stopped: make(chan struct{})}
	s.host = &windowHost{create: s.newWindow}
	webOpts := opts.Web
	webOpts.Chat, webOpts.ChatEphemeral = opts.Chat, false
	webOpts.Confirm = s.confirm
	webOpts.OpenExternal = func(url string) { _ = mygo.Shell.OpenExternal(url) }
	// The page follows the app's appearance, which Settings changes live.
	webOpts.Theme = "system"
	server, err := web.New(opts.Source, webOpts)
	if err != nil {
		return err
	}
	exe, err := s.prepareSupervisor(dataDir)
	if err != nil {
		return err
	}
	s.upgrade = newUpgradeWatch(exe, opts.Version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
		_ = s.applyOpenAtLogin(opts.Config.App.LaunchAtLogin)
		s.applyTray(opts.Config.App.Tray)
		if !startHidden(s.goos, mygo.App.WasOpenedAtLogin(), s.trayCreated()) {
			s.host.Open()
		}
		s.applyDock()
		supCtx, supCancel := context.WithCancel(ctx)
		s.stopCancel = supCancel
		go func() {
			defer close(s.stopped)
			if err := opts.Supervisor.Run(supCtx); err != nil {
				slog.Warn("collector did not stop cleanly", "event", "collector_stop_failed", "err", err.Error())
			}
		}()
		go s.watchNeeds(ctx)
		go s.upgrade.run(ctx, s.relaunch)
	})
	err = mygo.App.Run()
	s.stopCollector()
	return err
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
		sup.Start = func(ctx context.Context) (Child, error) {
			// Without the login environment the collector still runs, with
			// the app's own; the status says what is missing.
			env, err := LoadEnvironment(ctx, dataDir)
			s.mu.Lock()
			s.envErr = err
			s.mu.Unlock()
			if err != nil {
				slog.Warn("login environment unavailable", "event", "login_env_failed", "err", err.Error())
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

// stopCollector stops the supervisor and waits for its child to exit.
func (s *shell) stopCollector() {
	s.stopOnce.Do(func() {
		if s.stopCancel == nil {
			close(s.stopped)
			return
		}
		s.stopCancel()
	})
	<-s.stopped
}

// relaunch restarts the app on the upgraded executable, after its collector
// stopped so the new one starts from the new binary.
func (s *shell) relaunch() {
	slog.Info("executable upgraded; relaunching", "event", "app_relaunch")
	s.stopCollector()
	// The relaunched process starts as the app again, whatever launched this one.
	_ = os.Setenv("DRIGA_APP", "1")
	mygo.App.Relaunch()
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
	if st.Err == nil && s.envErr != nil && st.Collecting {
		st.Err = fmt.Errorf("without login shell PATH: %w", s.envErr)
	}
	return st
}

// watchNeeds keeps the tray and the badge on the current needs.
func (s *shell) watchNeeds(ctx context.Context) {
	last := int64(-1)
	ticker := time.NewTicker(needsPoll)
	defer ticker.Stop()
	for {
		version, err := s.opts.Source.DataVersion(ctx)
		if err == nil && version != last {
			if snap, err := s.opts.Source.LoadScope(ctx, store.Scope{}); err == nil {
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
	snap, err := s.opts.Source.LoadScope(ctx, store.Scope{})
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
		Title: "Driga", URL: "/", Width: 1280, Height: 820, MinWidth: 720, MinHeight: 480,
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

func (s *shell) applyOpenAtLogin(open bool) error {
	if mygo.App.OpenAtLogin() == open {
		return nil
	}
	err := mygo.App.SetOpenAtLogin(open)
	if err != nil {
		slog.Warn("could not change open at login", "event", "open_at_login_failed", "err", err.Error())
	}
	return err
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
	case CommandKeys:
		s.host.Navigate("/help/keys")
	case CommandDocs:
		go func() { _ = mygo.Shell.OpenExternal(DocsURL) }()
	default:
		if name, ok := strings.CutPrefix(command, "view:"); ok {
			s.host.Command("view", name)
		} else if raw, ok := strings.CutPrefix(command, "session:"); ok {
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
