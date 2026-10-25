# Driga Desktop App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `driga web` with Driga.app, a mygo desktop app that supervises the collector, sends notifications and shows the existing dashboard pages in a native shell.

**Architecture:** `internal/web` becomes a transport-free `http.Handler` with long-poll updates instead of server-sent events and no browser auth. `internal/desktop` (the only importer of mygo) serves that handler through mygo's `mygo` scheme, draws the native shell, supervises `driga collect` as a child that forwards notifications over a pipe, and posts them itself. The collector lock records its owner.

**Tech Stack:** Go 1.27.3, `github.com/egoist/mygo` v0.6.3 (purego, `CGO_ENABLED=0`), kong CLI, existing htmx pages.

**Spec:** `docs/superpowers/specs/2026-10-24-desktop-app-design.md`

## Global Constraints

- `CGO_ENABLED=0` for every target (darwin, linux, windows; amd64 and arm64).
- Only `internal/desktop` imports `github.com/egoist/mygo`.
- Tests run under `scripts/isolated.sh`; no test opens a window or touches real `~/.claude`, `~/.codex`, `~/.copilot`, `~/.gemini` or Driga data; no test runs a real agent CLI.
- Bundle identifier `run.driga`; app name "Driga"; command `driga app`; settings `app.tray` (default true) and `app.launch_at_login` (default true).
- Alpha: delete replaced code; no compatibility paths.
- Public repo: no personal paths or names in code, docs or fixtures.
- Comments, commits and docs in normal prose; commit trailer `Claude-Session: https://claude.ai/code/session_01T2vtcW6ihn1ywy9vxUq9a4`.

## Review Focus

1. Collector child dies repeatedly (bad config, schema newer): the app must back off, show the error in the tray menu and window, never spin. Test in Task 5.
2. Another collector holds the lock and later exits: the app must take over within one poll interval. Test in Task 5.
3. A long-poll request outlives the window or the data changes during it: the page must get exactly one refresh and no duplicate tail rows. Test in Task 2.
4. A notification arrives while no window exists: clicking it must create the window and select the session. Test in Task 6 (logic function, no display).
5. `driga` run from a process the app started must not open the app (`DRIGA_APP` leaked). Test in Task 7.

---

### Task 1: Collector lock records its owner

**Files:**
- Modify: `internal/collector/daemon.go` (Lock, lock file format)
- Modify: `internal/cli/collect.go` (hidden `--owner` flag), `internal/service/service.go` (service passes `--owner=service`), `internal/cli/doctor.go` (prints owner)
- Test: `internal/collector/lock_test.go`, `internal/cli/doctor_test.go`

**Interfaces:**
- Produces: `type Owner struct { PID int; Kind string; Executable string; Version string }` with `Kind` one of `"app"`, `"service"`, `"manual"`; `func Lock(dataDir string, owner Owner) (*os.File, error)` writes the owner as one JSON object; `func ReadOwner(dataDir string) (Owner, bool, error)` returns the holder only while the lock is held (checked with a nonblocking lock attempt that is released at once); `ErrAlreadyRunning` errors carry the owner (`fmt.Errorf("another driga collector is running (%s, pid %d): %w", kind, pid, ErrAlreadyRunning)`).
- `driga collect --owner` (hidden, default `"manual"`); `driga doctor` line `collector  ok  running (app, pid N)`.

- [ ] Write failing tests: `TestLockRecordsOwner` (Lock then ReadOwner returns the same Owner, PID = os.Getpid()); `TestReadOwnerWithoutHolder` (no lock held: ok=false); `TestLockBusyNamesOwner` (second Lock in a child process fails with an error containing `app, pid`); doctor test asserts the owner kind appears.
- [ ] Run `scripts/isolated.sh go test ./internal/collector ./internal/cli -run 'Lock|ReadOwner|Doctor'`; expect FAIL.
- [ ] Implement; update every `Lock(` caller (`collector.Run`, `cmd/driga-demo`, tests) to pass an Owner.
- [ ] Rerun; expect PASS. Run `scripts/isolated.sh go test ./...`.
- [ ] Commit `feat(collector): record who holds the collector lock`.

### Task 2: Web handler without browser transport

**Files:**
- Modify: `internal/web/server.go`, `internal/web/chat.go`, `internal/web/pages.go`, `internal/web/static/app.js`, `internal/web/static/chat.js`, `internal/web/static/flow-board.js`, `internal/web/templates/*.html`
- Delete: `web.Listen`, `Server.URL`, token, csrf, cookie, Host and Origin checks, `/events`, `/chat/api/events`, the htmx SSE extension `static/vendor/htmx-ext-sse-2.2.2.js` if no longer referenced, and their tests (`server_test.go` cases for token, host, origin, csrf; `sse_stream_lifecycle_test.go`)
- Test: `internal/web/updates_test.go`, `internal/web/chat_wait_test.go` (extend), existing page tests

**Interfaces:**
- Produces: `func New(source Source, opts Options) (*Server, error)` with `Options.Host` removed; `Server` implements `http.Handler`; `func (s *Server) Watch(ctx context.Context)` keeps the one-second `DataVersion` check but wakes long-poll waiters instead of SSE clients.
- `GET /updates?after=<cursor>&<page query>` blocks until the data version or activity tail changes after `cursor`, or 25 s pass, then returns `application/json` `{"cursor": "<opaque>", "refresh": bool, "tail": [view.TailEvent...]}`. A first call without `after` returns at once with the current cursor and no refresh.
- `GET /chat/api/wait?id=<id>&after=<seq>` returns `chat.State` from `Client.Wait` (blocks until a newer sequence or 25 s).
- `Options.Confirm func(ctx context.Context, message string) bool` and `Options.OpenExternal func(url string)`: nil in tests; the page calls them through `POST /app/confirm` and `POST /app/open-external` (JSON `{"message"}` / `{"url"}`), which return 404 when nil. The kill action keeps its server-side `Confirmed` requirement.
- Page scripts poll `/updates` and `/chat/api/wait` in a loop with one request in flight, back off 1 s on error, and stop on `pagehide`. All fetches use relative URLs. No `X-CSRF-Token`, no `chat-csrf` meta, no `EventSource`.
- CSP, `X-Content-Type-Options`, `Referrer-Policy`, `Cache-Control` headers and the local-session check for actions stay.

- [ ] Write failing tests: `TestUpdatesReturnsCursorAtOnce`; `TestUpdatesWakesOnDataVersion` (fake source bumps version; response has refresh=true within 2 s); `TestUpdatesTimesOut` (with a 50 ms test timeout field, returns refresh=false); `TestUpdatesNoDuplicateTail` (two consecutive calls never repeat a tail key; Review Focus 3); `TestChatWaitReturnsNewerSequence`; `TestNoAuthOrHostChecks` (request with any Host and no cookie gets 200 on `/`); `TestScriptsHaveNoEventSource` (static assets contain no `EventSource`, `X-CSRF-Token`, `chat-csrf`, `localStorage` theme key); `TestAppConfirmNotFoundWithoutHook`.
- [ ] Run `scripts/isolated.sh go test ./internal/web`; expect FAIL.
- [ ] Implement server side, then rewrite the three scripts' transport. Keep page behaviour (refresh, tail rows, chat deltas) identical.
- [ ] Rerun web tests and the golden flow tests (`UPDATE_FLOW_GOLDEN` only if output changes intentionally); expect PASS.
- [ ] Commit `refactor(web): serve pages without a browser transport`.

### Task 3: Page drops app-shaped HTML

**Files:**
- Modify: `internal/web/templates/dashboard.html`, `board.html`, `internal/web/static/app.js`, `flow-board.js`, `internal/web/pages.go`
- Test: `internal/web/polish_test.go` (extend)

**Interfaces:**
- Consumes: `Options.Confirm` from Task 2.
- Produces: `Options.Theme string` (`"system"`, `"light"`, `"dark"`) rendered into the page; the page exposes `window.drigaCommand(name, arg)` with names `view` (`flow`, `projects`, `machines`, `chat`), `find`, `reload`, `openSession` (session id), which call the same functions the page keys call.
- Removed: header tab bar, footer key legend (content moves to a `keyboard` page at `/help/keys` that the Help menu opens in the window), theme `<select>`, the `theme` command-bar verb, the `localStorage` theme, `confirm()` calls (now `POST /app/confirm`), the Inbox "copy command" fallback (replaced by a Jump button posting the existing `jump` action), the `· driga console` title suffix.

- [ ] Write failing tests: rendered `/` has no `<nav` tab bar, no key legend footer, no theme select, `<title>` exactly `Driga`; `/help/keys` lists the keys previously in the legend; `drigaCommand` exists in app.js; no `confirm(` in static scripts.
- [ ] Run; expect FAIL. Implement. Rerun; expect PASS. Regenerate goldens if they change (`UPDATE_FLOW_GOLDEN=1`, `UPDATE_MACHINE_GOLDEN=1`) and review the diff.
- [ ] Commit `refactor(web): leave app chrome to the native shell`.

### Task 4: Collector forwards notifications to its parent

**Files:**
- Create: `internal/attention/pipe.go`
- Modify: `internal/cli/collect.go` (hidden `--notify-fd int`), `internal/cli/deps.go`, `cmd/driga/main_darwin.go`
- Test: `internal/attention/pipe_test.go`, `cmd/driga/main_darwin_test.go`

**Interfaces:**
- Produces: `type PipeNotifier struct { W io.Writer }` implementing `Notifier`; writes one JSON line per `Notification` (fields `kind,title,body,sound,item_id,machine_id`) under a mutex. `func ReadNotifications(r io.Reader, fn func(Notification)) error` decodes lines until EOF.
- `driga collect --notify-fd N`: deps use `PipeNotifier{W: os.NewFile(N, "notify")}` instead of `DesktopNotifier`; macOS skips `RunNotificationMain` (no AppKit) when the flag is present. Expose the decision as `func usesAppKit(args []string) bool` in `cmd/driga` for the test.

- [ ] Write failing tests: round trip PipeNotifier → ReadNotifications; concurrent Notify calls produce whole lines; `usesAppKit([]string{"collect"}) == true`, `usesAppKit([]string{"collect","--notify-fd","3"}) == false`, `usesAppKit([]string{"top"}) == false`.
- [ ] Run; FAIL. Implement. Run; PASS.
- [ ] Commit `feat(collector): forward notifications to a parent process`.

### Task 5: Supervisor in internal/desktop

**Files:**
- Create: `internal/desktop/supervisor.go`, `internal/desktop/env.go`
- Test: `internal/desktop/supervisor_test.go`, `internal/desktop/env_test.go`

**Interfaces:**
- Consumes: `collector.ReadOwner`, `collector.Owner` (Task 1); `attention.ReadNotifications` (Task 4); `chat.SocketPath`.
- Produces:
  - `type Supervisor struct { Start func(ctx context.Context) (Child, error); ReadOwner func() (collector.Owner, bool, error); Ready func(ctx context.Context) error; Notify func(attention.Notification); Status func(Status); Backoff []time.Duration; Poll time.Duration }` with `func (s *Supervisor) Run(ctx context.Context) error`.
  - `type Child interface { Wait() error; Stop() error; Notifications() io.Reader }`; production `execChild` runs `<exe> collect --owner=app --notify-fd=3` with `ExtraFiles` and, on Windows, `CREATE_NO_WINDOW`.
  - `type Status struct { Collecting bool; Owner collector.Owner; Err error }`.
  - Rules: lock free → start child; owner kind `app` with a live PID and the same executable → adopt (watch, stop on quit); other owner → report it and poll every `Poll`; child exits unexpectedly → restart after the next `Backoff` step (default 1 s, 2 s, 5 s, 10 s, 30 s, then 30 s), reset after 5 minutes healthy; context cancel → `Stop` the child and wait.
  - `Ready` polls the chat socket until it answers (default timeout 10 s); the app opens chat only after it.
  - `func LoginEnvironment(ctx context.Context) (map[string]string, error)` runs the user's login shell (`$SHELL -l -c env`, 5 s timeout) once at first launch and keeps the same keys `service.go` keeps; stored in the app's data dir as `app-env.json` (0600) and passed to the child.

- [ ] Write failing tests with fake `Start`/`ReadOwner` and a fake clock: starts when free; adopts own orphan; waits and takes over when another owner exits (Review Focus 2); backs off on repeated exits and reports the error (Review Focus 1); stops child on cancel; notifications from the child reach `Notify`; `LoginEnvironment` filters keys (fake shell script under the isolated HOME).
- [ ] Run `scripts/isolated.sh go test ./internal/desktop`; FAIL. Implement. PASS.
- [ ] Commit `feat(desktop): supervise the collector as a child process`.

### Task 6: Native shell

**Files:**
- Create: `internal/desktop/app.go` (mygo wiring), `internal/desktop/menu.go`, `internal/desktop/tray.go`, `internal/desktop/notify.go`, `internal/desktop/settings.go`, `internal/desktop/count.go`, `internal/desktop/assets/tray*.png`
- Test: `internal/desktop/menu_test.go`, `internal/desktop/count_test.go`, `internal/desktop/notify_test.go`

**Interfaces:**
- Consumes: `web.New`, `web.Options{Confirm, OpenExternal, Theme}` (Tasks 2–3), `Supervisor` (Task 5).
- Produces: `func Run(opts Options) error` (called on the main goroutine) with `Options{ Source web.Source; Chat chat.Client; Supervisor *Supervisor; Config config.Config; Version string }`.
- Pure functions tested without a display: `func MenuModel(goos string) []MenuSpec` (labels, roles, accelerators per the spec: Cmd on darwin, Ctrl elsewhere); `func CountLabel(n int) string` (`""` for 0, `"9+"` above 9); `func TrayMenuModel(needs []view.Need, status Status) []MenuSpec` (Open Driga, first five needs, status line, Quit); `func ClickTarget(n attention.Notification) (sessionKey string)`; `func CloseQuits(goos string, trayCreated bool) bool` (darwin false; elsewhere `!trayCreated`).
- mygo wiring: `mygo.Protocol.Handle("mygo", handler)`; one window titled "Driga" with remembered bounds; `RequestSingleInstanceLock`, `OnSecondInstance` focuses; Dock policy regular while a window is open, accessory when none; `SetBadgeCount`; tray with template icon (macOS title = count; Windows icon variant with a dot and tooltip count); native confirm dialog for `Options.Confirm`; `OpenExternal` via the OS opener; notifications via `mygo.NewNotification` whose click creates or focuses the window and calls `drigaCommand("openSession", key)` (Review Focus 4); Settings window built with `github.com/egoist/mygo/ui` (theme, tray, launch at login) writing config through `config` package; `App.SetOpenAtLogin` from `app.launch_at_login`; upgrade watch: every 60 s compare the executable's modification time and embedded version, on change stop the supervisor and `App.Relaunch()`.

- [ ] Write failing tests for the pure functions above.
- [ ] Run; FAIL. Implement pure functions; PASS.
- [ ] Implement mygo wiring. Build with `CGO_ENABLED=0` for darwin, linux, windows.
- [ ] Commit `feat(desktop): native shell for the dashboard`.

### Task 7: CLI entry points

**Files:**
- Create: `internal/cli/app.go`, `cmd/driga-app/main.go` (Windows GUI build)
- Modify: `internal/cli/root.go` (remove `Web`, add `App`), `cmd/driga/main.go`, `cmd/driga/main_darwin.go`
- Delete: `internal/cli/web.go`, `WebOptions`, `openWebBrowser` and their tests
- Test: `internal/cli/app_test.go`, `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `desktop.Run` (Task 6), `desktop.Supervisor` (Task 5).
- Produces: `type AppCmd struct{}`; `func appLaunch(env func(string) string, openedAtLogin bool, args []string) bool` decides app mode: true when `args` is `["app"]`, or when no args and (`DRIGA_APP=1` or `openedAtLogin`). After reading, the process unsets `DRIGA_APP` (Review Focus 5).
- On darwin, `driga app` from a path outside a bundle runs `open -b run.driga` and exits; inside the bundle it runs the app.
- `cmd/driga-app` is a Windows-only main built with `-ldflags -H=windowsgui` into `Driga.exe`; it always runs app mode.

- [ ] Write failing tests: `appLaunch` table (bare+env → true; bare+login → true; `["top"]` → false; bare with env unset → false; `["app"]` → true); after app mode starts, `os.Getenv("DRIGA_APP") == ""`; `driga web` is an unknown command; `driga --help` lists `app`.
- [ ] Run; FAIL. Implement; PASS. `scripts/isolated.sh go test ./...`.
- [ ] Commit `feat(cli): open the desktop app with driga app`.

### Task 8: Packaging

**Files:**
- Modify: `scripts/macos-app.py` (drop `LSUIElement`, add `LSEnvironment` `{DRIGA_APP: "1"}`, `NSHighResolutionCapable`), `scripts/verify-macos-app.py`, `scripts/macos_app_test.go`, `.goreleaser.yaml` (cask installs the app to `/Applications`, caveats without `driga service install`; Windows archive with `Driga.exe` and `bin/driga.exe`; Linux nfpm `recommends` GTK3, WebKitGTK and AppIndicator, installs `driga.desktop`), `scripts/verify_release_dist_test.go`
- Create: `packaging/linux/driga.desktop`

**Interfaces:**
- Release artifacts keep their current names except the Windows archive gaining `Driga.exe`.

- [ ] Write failing tests: plist has `LSEnvironment.DRIGA_APP == "1"` and no `LSUIElement`; dist verifier expects `Driga.exe` in the Windows archive; desktop file has `Exec=driga app` and `StartupWMClass=run.driga`.
- [ ] Run; FAIL. Implement; PASS. Run `goreleaser check`.
- [ ] Commit `build: package Driga.app, Driga.exe and a Linux desktop entry`.

### Task 9: Docs, site and footage

**Files:**
- Modify: `README.md`, `docs/usage.md`, `site/src/content/docs/**` (regenerate reference with `go run ./cmd/driga-docgen`), landing components that show web films, `site/footage/*` (drop the browser recorder from `just site-footage`; keep TUI recording), `Justfile`
- Test: `just check` (docgen check, site build)

- [ ] Replace every `driga web` mention with the app and `driga app`; describe app + CLI, the collector child, `driga service install` for headless machines.
- [ ] Remove landing sections that show browser footage of `driga web` until real app footage exists; keep TUI films. Captions must match what is shown.
- [ ] Run `go run ./cmd/driga-docgen` then `just check`; expect PASS.
- [ ] Commit `docs: Driga is a desktop app plus a CLI`.

### Task 10: End-to-end check on demo data

**Files:** none (verification only)

- [ ] Build `CGO_ENABLED=0 go build ./cmd/driga`; package with `scripts/macos-app.py` into a scratch `Driga.app`.
- [ ] In an isolated environment with `cmd/driga-demo`, launch the bundle with `open`; verify: one Driga process with AppKit (child has none), window renders, a need appears and Allow clears it, the tray shows the count, the Dock badge matches, a notification click opens the session, Quit stops the child.
- [ ] Run the full suite: `scripts/isolated.sh go test ./...`, `TZ=UTC scripts/isolated.sh go test ./...`, cross-builds for linux and windows with `CGO_ENABLED=0`.
