# Needs-you inbox: step 1 implementation handoff

Scope: sections 4.1–4.7 and the post-builder decisions in the v1.8 control design.
Steps 2–5 and Gemini hooks are deferred. No new Go dependency, README change,
commit, push, or live hook installation is part of this change.

## Behavior delivered

- Claude notifications carry `notification_type`, tool metadata and policy-bounded
  detail in the existing spool. Known types map to permission/question/elicitation;
  absent or future types remain unknown. `idle_prompt` produces neither a wait
  nor a needs-input alert. Reading an existing waiting signal on first collection
  never manufactures an acknowledgement.
- The collector optionally polls `claude agents --json`. A CLI-only blocked
  session creates a wait. Hook and CLI reasons are retained separately so known
  hook reasons win and CLI reasons can keep updating when the hook is unknown.
  A nonblocked CLI report never closes a hook wait. Durable generation aliases
  prevent repeated polls from reopening an acknowledged or automatically ended wait.
- One wait retains its initial machine/session/start identity. Subsequent
  notifications update reason/detail. Automatic ends use `ended_at`; human acks
  use `acked_at`. Explicit same-session user signals close any reason. Later
  same-session activity, Stop/tool results and observed transcript growth close
  permission/question/elicitation waits. Worker evidence does not close a parent.
  A late hook and later growth drained in one pass are handled chronologically.
- Migration 0011 is additive. It stores the new inbox fields, separate internal
  hook/CLI reasons, and controlling TTYs. Legacy worker waits recover their own
  session from their natural key. Repeated active local legacy notifications keep
  the earliest row active and preserve the redundant rows as ended history;
  no acks are created. Alert/alias metadata follows inbox retention.
- Shared read projections order active waits oldest first and completions/history
  newest first. `waited_ms` stops at the earlier end/ack. Default reads omit ended
  rows; CLI `--all` and MCP `all:true` include history. Schema remains 2.
- TUI tab 3 shows reason, tool, duration, project/branch and title/detail; Enter
  uses the proved jump and x acknowledges. Home/Flow show waiting count and oldest
  wait; lead/worker flow boxes show reasons. The web inbox shows the same metadata
  and a readonly, copyable jump command. It has no mutation endpoint.
- TTY samples match an injectable tmux pane listing. TUI switches clients only
  for a local item in the same proved server; otherwise it prints the attach
  command. Remote items show the machine and command without opening a shell.
  Terminal/iTerm2 window selection requires both TTY proof and `jump_terminal`.
  A project directory and safely quoted resume command provide the fallback.
- Fleet snapshots/feed retain reason, wait start, source, end and jump fields.
  Automatic ends do not queue remote acks; explicit pending acks survive a
  concurrent automatic end. Remote desktop alerts require a paired-machine
  entry in the viewing machine's trust file.
- `none` exports no tool/detail; `titles` permits tool names but omits pending
  commands/questions; `snippets` scrubs and caps detail at 200 characters. Capture
  is reapplied at read, alert and export boundaries. Resume/attach commands are
  navigation metadata, separate from captured pending tool commands.

## Official documentation checked

Checked against the live documentation during this implementation:

- [Claude Code hooks reference](https://code.claude.com/docs/en/hooks#notification):
  Notification input explicitly includes `notification_type`, `message`, and an
  optional title. The matcher list includes permission_prompt, idle_prompt,
  agent_needs_input and elicitation types. No settings matcher change is needed.
- [Codex hooks](https://learn.chatgpt.com/docs/hooks): PermissionRequest can decline
  to decide and continue normal approval handling; every command hook receives
  JSON on stdin. Inline `[[hooks.PermissionRequest]]` tables in config.toml are
  documented. Timeouts are seconds. New/changed nonmanaged hooks require review
  and trust through Codex `/hooks` before they execute.

Codex waits therefore ship for TOML shapes that pass the byte-preserving append
and round-trip checks. `driga hooks install codex` (also existing agent selection)
previews and asks before writing. The owned report-only hook has a 3-second
external timeout, writes one spool event and returns success with empty stdout;
stdin is bounded by the existing 2-second read budget. Comments, unrelated tables,
large integers, absent final newlines and quoted binary paths are covered by
round-trip tests. Existing foreign notify ownership retains its previous refusal;
incompatible inline TOML shapes or changed owned blocks are refused without a
full-file rewrite. Existing rollout completions continue to work.

Could not independently verify the `claude agents --json` wire shape in public
primary documentation: searches for `waitingFor` and this JSON command returned
no matching reference. The implementation accepts fixture-backed list/agents
wrapper shapes, id/sessionId/session_id identities and string/type waitingFor;
unrecognized reasons remain unknown, malformed output is skipped. No live agent
configuration, real tmux server, notification centre, or terminal app was used for
integration testing. Native notification delivery, OS privacy permissions, and
installed-release hook trust/behavior still need an operator smoke test.
No adapter-owned vendor remote URL was available, so none is fabricated.

## Judgment calls

| Area | Decision |
| --- | --- |
| Polling | One 200 ms deadline per full collection pass; no CLI poll on spool-only wakes. Missing, malformed or slow CLI results are skipped optional reads. Stdout is capped at 1 MiB and recursive hooks are suppressed. |
| Jump budget | One shared 100 ms discovery budget per pass. The current/default tmux server supplies its socket path as proof for same-server switching. Missing proof falls back to a copyable command. |
| Notifications | Injectable osascript notifier on macOS, notify-send on Linux when installed; one-second native delivery budget, best effort and no retry storm. User text travels as argv, never interpolated into AppleScript. |
| Defaults | alerts=waiting, alert_after=0s, alert_repeat=5m, jump_terminal=false. The zero quiet interval preserves immediate alerts until held-out evidence validates a delay. No maintainer live data was used for tuning. |
| Quiet/throttle | Quiet detection uses same-session activity immediately preceding the wait; one persistent item receipt and one per-session repeat window cover both waiting and finished kinds. Receipts are recorded before delivery, favoring at-most-once behavior when a native notifier fails. |
| Summary | Starts within three seconds in one committed batch produce a single count-only summary. Separate passes keep immediate delivery; there is no artificial batching delay. |
| Distinct kinds | Needs input uses Glass on macOS / message-new-instant hint on Linux; Done uses Pop / complete. Finished alerts require alerts=all. Desktop settings may choose how to honor sounds. |
| Presentation | Preserve the existing pane layout, typography, density and waiting tokens. TUI uses bracketed reason chips; web uses existing chip classes and a keyboard-focusable readonly command field. No new visual system or frontend dependency. |
| Migration | New hook fields stay in JSONL; migration 0011 covers inbox/process persistence. Legacy completion import behavior stays intact; waiting signals require the explicit end rules. |
| Vendor links | Resume fallback until an adapter provides a proven URL; no guessed vendor URL. |

## Tests and verification

Regression tests were observed failing before their fixes for notification reasons
and idle handling, worker identity, config loading, Codex stdin/report-only input,
byte-preserving install and positional syntax, wait merging and automatic ends,
first-collection acknowledgements, CLI generation replay after ack, reason-source
precedence, same-pass growth, remote ack/alert ownership, schema constraints,
rendered ordering/durations, jumps, sound/throttle behavior and metadata retention.
Additional contract coverage checks CLI/MCP parity and fleet policy-bounded fields.

Tests use temp HOMEs, DRIGA_DATA_DIR, DRIGA_SPOOL_DIR and DRIGA_CONFIG where paths
are involved. CLI snapshot tests install a fake command runner; collector, tmux,
notifier and terminal tests inject fakes. Servers use test fixtures and cleanup.
No live hook installer was run. No process-termination test was run.

Required verification commands (final results recorded below):

```text
gofmt clean + git diff --check
go vet ./...
go test ./internal/derive ./internal/store ./internal/collector ./internal/hooks ./internal/intake ./internal/view ./internal/tui ./internal/web ./internal/cli ./internal/mcp ./internal/fleet ./internal/feed ./internal/config ./internal/inbox
go test -race <the same package list>
GOOS=linux go build ./...
```

The full `go test ./...` suite was not run: it includes process-termination tests,
which this task forbids. To check the stated macOS sandbox boundary without
signalling anything, the six selected native tests TestProcsIncludesSelf,
TestAliveAcceptsSelf, TestAliveUsesAsymmetricWindow, TestCwdsSelf,
TestCwdsAllOwnProcessesQuickly and TestProcsRescanReusesIdentity were run; all passed
in this environment. No sandbox-only failure was observed in those checks.
The fixture-only TestControllingTTYFromStat also passed.

Final results:

- **1,081 tests passed** across the 13 requested packages plus internal/inbox.
- The complete requested package set plus internal/inbox passed **with -race**
  (1,078 cases at that run). The last first-collection fix passed a subsequent
  full derive/collector race run (256 cases); the final optional-source and remote
  quiet-window fixes passed targeted derive/collector race follow-ups (17 cases).
  The final normal run above includes all added regression cases.
- `go vet ./...`, `GOOS=linux go build ./...`, gofmt cleanliness and
  `git diff --check` passed on the final implementation.
- The controlling-TTY fixture passed normally and with -race. The six selected
  native checks above passed; no unobserved full-suite failure is claimed.
- README.md, go.mod and go.sum are unchanged. No commit or push was made.

The final review also keeps unavailable optional CLI polling out of intake-health
warnings and uses paired sessions' own activity for remote quiet-window checks.

## Files changed

- `.env.example`
- `docs/contracts/schema2.schema.json`
- `docs/superpowers/reports/2026-10-19-needs-you-step1.md`
- `docs/superpowers/specs/2026-10-19-canon-contracts.md`
- `internal/cli/deps.go`
- `internal/cli/hook.go`
- `internal/cli/hook_test.go`
- `internal/cli/hooks.go`
- `internal/cli/hooks_test.go`
- `internal/cli/inbox_runner_test.go`
- `internal/cli/needs_you_test.go`
- `internal/cli/open.go`
- `internal/cli/read.go`
- `internal/cli/top.go`
- `internal/collector/daemon.go`
- `internal/collector/inbox.go`
- `internal/collector/intake.go`
- `internal/collector/needs_you_test.go`
- `internal/collector/pass.go`
- `internal/collector/sources.go`
- `internal/config/config.go`
- `internal/config/inbox_test.go`
- `internal/config/settings.go`
- `internal/derive/inbox.go`
- `internal/derive/inbox_test.go`
- `internal/derive/needs_you_test.go`
- `internal/feed/feed.go`
- `internal/feed/needs_you_test.go`
- `internal/hooks/codex.go`
- `internal/hooks/needs_you_test.go`
- `internal/inbox/inbox.go`
- `internal/inbox/inbox_test.go`
- `internal/inbox/terminal.go`
- `internal/jsonwire/marshal.go`
- `internal/mcp/needs_you_test.go`
- `internal/model/model.go`
- `internal/platform/platform_darwin.go`
- `internal/platform/platform_linux.go`
- `internal/platform/tty_test.go`
- `internal/platform/tty_unix.go`
- `internal/store/ack.go`
- `internal/store/intake.go`
- `internal/store/migrations/0011_needs_you.sql`
- `internal/store/needs_you_migration_test.go`
- `internal/store/needs_you_test.go`
- `internal/store/queries.go`
- `internal/store/remote.go`
- `internal/tui/app.go`
- `internal/tui/flow.go`
- `internal/tui/header.go`
- `internal/tui/inbox.go`
- `internal/tui/mouse.go`
- `internal/tui/needs_you_test.go`
- `internal/tui/tabs.go`
- `internal/view/builders.go`
- `internal/view/dashboard.go`
- `internal/view/fleet.go`
- `internal/view/flow.go`
- `internal/view/needs_you_test.go`
- `internal/view/schema_test.go`
- `internal/view/view.go`
- `internal/view/workers_test.go`
- `internal/web/needs_you_test.go`
- `internal/web/pages.go`
- `internal/web/templates/dashboard.html`
- `internal/web/templates/flow.html`
