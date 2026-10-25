# Driga desktop app

## Intent

Driga's job is to show what needs you. Today the graphical view is `driga web`:
a foreground loopback server on a random port, protected by a bearer token,
cookie, Host allow-list, Origin check and CSRF token. It is clunky (a terminal
stays busy, the URL changes every run, nothing can be bookmarked) and its
security exists only because a TCP port is reachable by other local users and
by pages in the browser.

A desktop app removes the port and, with it, the browser security layer. It
runs as the user, so it can do only what the user can already do. It gives
Driga a Dock badge, a menu-bar item, native menus and notifications that open
the session they are about, which suit a tool whose purpose is attention.

Driga becomes an app plus a CLI, like Docker Desktop with `docker` or VS Code
with `code`. Desktop users install and run Driga.app; the `driga` CLI (TUI,
`summary`, `doctor`, MCP, scripting) comes with it. Headless machines install
the CLI alone.

## Goals, non-goals, anti-goals

- Goal: one app on desktops that collects, notifies and shows the dashboard.
- Goal: nothing app-shaped drawn in HTML; nothing web-shaped (ports, tokens,
  cookies, CSRF, browser storage) left in the app.
- Non-goal: a native rewrite of the dashboard content.
- Non-goal: remote or phone access to the dashboard.
- Anti-goal: an app fault that stops collection, or two collectors competing.
- Anti-goal: a second copy of every notification.

## Decisions

### Framework

[mygo](https://github.com/egoist/mygo) (MIT, v0.6.x). It calls native APIs
through `purego`, so `CGO_ENABLED=0` still builds every target. Its `Protocol`
serves a URL scheme from an `http.Handler`.

A throwaway spike (branch `spike/desktop-app`) proved on demo data: the
unchanged dashboard renders in a mygo window with no TCP port; server-sent
events update the page live on macOS; Allow posts `/chat/api/prompt-answer`
and the session leaves Needs; the tray item appears; macOS, Windows and Linux
build with `CGO_ENABLED=0`.

Fallback: Wails v3, which needs cgo on macOS and Linux. Only
`internal/desktop` imports the framework, so a switch touches one package.

### Content web, shell native

The board, map, timeline, sessions, inbox and chat panes stay the
`internal/web` pages in the system webview (WKWebView, WebView2, WebKitGTK).
Window, menus, shortcuts, tray, badge, dialogs, settings, notifications, single
instance and launch at login are native.

### Live updates use mygo events

mygo on Windows buffers a scheme response until the handler returns
(`internal/windows/webview.go`, "WebView2 wants it whole"), so `EventSource`
cannot stream there. The page's live updates move from server-sent events to a
mygo channel or typed event on every platform: Go pushes "data changed" and
chat deltas, the page fetches what it needs with ordinary requests. The
`/events` and chat SSE endpoints are deleted.

### The app owns the collector

- Driga.app runs `driga collect` as a supervised child: started at launch,
  restarted with backoff when it exits unexpectedly, stopped on Quit.
- The child runs with notifications off (`collect --notify=false`, or an
  equivalent internal switch), so it never starts AppKit. There is one AppKit
  process, the app.
- The child's environment is the one `driga service install` captures today
  (PATH, TMUX_TMPDIR, path overrides; `internal/service/service.go`), recorded
  by the app at first launch from a login shell, so agent discovery and
  terminal jumps work from a Finder launch.
- The app opens chat only after the child's chat socket answers (readiness
  handshake), so `chat.Open` never falls back to an ephemeral broker.

### Collector ownership

`collector.Lock` records, next to the PID: owner kind (`app`, `service`,
`manual`), the executable path and the driga version. Rules:

- The app adopts an orphaned collector of kind `app` (left by an app crash) and
  stops it on Quit.
- When another owner holds the lock, the app shows that owner's data, says who
  is collecting, and polls; when the holder exits, the app starts its child.
- `driga doctor` names the owner.
- On macOS desktops the app's first launch uninstalls a launchd service this
  user installed (the code `driga service uninstall` runs), after saying so in
  the window. Alpha: no other migration.

### Notifications

The app sends notifications through mygo, from the same rules the collector
uses today (`internal/attention`), with bundle identity `run.driga`, so the
permission already granted carries over. A click opens Driga on that session
(window to front, session selected, its prompt focused). Windows and Linux get
the same click behaviour. The headless service keeps its own notifier.

### Upgrades

The app watches its executable's version. When `brew upgrade` (or the Windows
installer) replaces it, the app stops its child and relaunches itself, so an
old app never reads a store migrated by a newer collector (`ErrSchemaNewer`).
If the quit is cancelled, no collector starts under the old app: the tray
says "Restart Driga to finish updating" and the app asks again on its next
check.

### Executables

- macOS: one binary, `Driga.app/Contents/MacOS/driga`, the CLI and the app.
  Homebrew links it as `driga`. `driga app` hands off to Launch Services
  (`open -a` with the bundle's path) so the login item and notification
  identity are the bundle's.
- Windows: `Driga.exe` (GUI subsystem, the app) and `bin\driga.exe` (console
  subsystem, the CLI), built from the same packages, like VS Code's `Code.exe`
  and `code`. The collector child is started with `CREATE_NO_WINDOW`.
- Linux: one binary; `driga app` opens the app.

### Removed

`internal/cli/web.go`, the `Web` command, `WebOptions`, `web.Listen`,
`Server.URL`, `Server.Watch`'s SSE fan-out, the `/events` and chat stream
endpoints, the `token`, `csrf`, `cookieName`, `instanceCookie` and Host
checks, the Origin/CSRF branch in `chat.go`, the `chat-csrf` meta tag and its
readers in `app.js`, `chat.js` and `flow-board.js`, `openWebBrowser`, their
tests, and every `driga web` mention in README, docs and the site. The CSP and
the local-session authorization of actions (`chat.go`, session must be local)
stay. Alpha: no compatibility path.

## Naming

| Thing | Name |
|---|---|
| Product and app | Driga |
| macOS bundle | `/Applications/Driga.app`, bundle identifier `run.driga` |
| Windows | `Driga.exe`, `bin\driga.exe`, AppUserModelID `run.driga`, Start menu "Driga", winget `LabStack.Driga` |
| Linux | `driga.desktop`, application ID `run.driga` |
| Open the app | `driga app` (opens Driga, or focuses it) |
| Finder, Dock, Spotlight launch | Info.plist `LSEnvironment` `DRIGA_APP=1`; the app unsets it after reading, so processes it starts see a normal environment |
| Login launch | mygo's `WasOpenedAtLogin()` selects app mode explicitly on every platform |
| Collector | `driga collect`: the app's child on desktops; `driga service install/start/stop/restart/status` on headless machines |
| Packages | `internal/desktop`: app shell and collector supervision; `internal/web`: pages and handler, transport-free; `internal/actions`, `internal/attention`: unchanged roles |
| Settings | `app.tray` (bool, default true), `app.launch_at_login` (bool, default true, registered at first launch); theme stays `ui.theme` |

## Native shell

- **Window.** Title "Driga". Size and position remembered.
- **Close and Quit.** Closing the window keeps Driga collecting while it has a
  visible icon: the menu bar on macOS, or a tray that was actually created on
  Windows and Linux (mygo returns an error when no tray host exists). With no
  visible icon, closing quits. Quit (menu, tray, Cmd+Q) stops the window and
  the collector child.
- **Dock.** The Dock icon shows while a window is open (regular activation
  policy) and hides when it closes (accessory policy); a login launch starts
  hidden in the menu bar.
- **Menu bar.** Driga (About, Settings… Cmd+,, Quit), Edit (standard roles;
  Find wired to the page's find field), View (Flow Cmd+1, Projects Cmd+2,
  Machines Cmd+3, Chat Cmd+K, Reload Cmd+R, zoom), Window (standard), Help
  (Keyboard Shortcuts, Documentation opens driga.dev in the browser). Ctrl
  replaces Cmd on Windows and Linux. Menu items send the page's existing
  commands (the same functions its `m`, `t`, `s`, `/` keys call).
- **Tray.** An icon only, never text: the Driga mark (a template on macOS),
  with a dot while anything needs you. Menu: Open Driga, the first five needs (each opens that
  session), collector status ("Collecting", or who else is, or the error),
  Quit. Off with `app.tray = false`.
- **Count.** Dock badge on macOS, launcher badge on Linux where supported. On
  every platform the tray tooltip and the tray menu's first line say how many
  need you.
- **Dialogs.** Stop asks with a native dialog parented to the window ("Stop
  ‹title›?" Stop / Cancel). Every `confirm()` in the page becomes a native
  dialog.
- **Settings window.** mygo's own UI widgets (not HTML): theme, tray, launch at
  login. Writes the config the CLI reads.
- **External links** open in the default browser.
- **Single instance.** A second launch, `driga app` included, focuses the
  running window and exits.

## Page changes

- The header tab bar goes; the View menu and the page keys switch views. The
  page keeps its in-page view switcher.
- The footer key legend moves to Help → Keyboard Shortcuts.
- The theme `<select>`, the command bar's `theme` verb and the
  `localStorage` theme go; the page takes `ui.theme` from the app.
- The Inbox "copy command" fallback becomes a native jump action.
- The `<title>` suffix "· driga console" goes.
- Relative URLs only, so the Windows origin (`http://mygo.localhost`) works.

## Distribution

- **macOS:** the cask installs `/Applications/Driga.app` and links the CLI.
  Its caveats stop telling users to run `driga service install`. First launch
  asks for notifications (if not yet granted) and registers the login item.
- **Windows:** one installer for Driga and the CLI on PATH.
- **Linux desktops:** the CLI package recommends (not requires) GTK3,
  WebKitGTK and an AppIndicator library, and installs `driga.desktop`. Best
  effort in the first release.
- **Headless:** the CLI alone, with `driga service install`.

## Constraints

- `CGO_ENABLED=0` for every target, checked in CI.
- Go 1.27.3.
- Tests stay isolated (scripts/isolated.sh); no test opens a window. The web
  handler is tested over `httptest`; `internal/desktop` keeps its logic
  (supervision, ownership rules, menu model, count, launch-mode selection) in
  plain functions that test without a display.
- Footage stays real recordings of the shipped binary on demo data. The first
  release records the TUI only; app footage follows once the app ships.

## Risks

- mygo is pre-1.0 with one main maintainer.
- Linux tray needs a StatusNotifierItem host; GNOME needs an extension.
- Binary size grows (spike: 40 MB unstripped against 26 MB released); measure
  with release flags.
- Two AppKit processes would misroute clicks and launches; the design keeps
  one, and the plan must test that the child never initializes AppKit.

## Left to the builder

Menu item order within the rules above, tray artwork, the mygo mechanism for
page events (channel or typed event), and the backoff numbers for child
restarts.
