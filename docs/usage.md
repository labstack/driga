# Using Driga

## Everyday commands

```sh
driga agents --all                 # include sessions ended within 24 hours
driga agents --json --project .
driga agents show <id>                   # numeric ID from the agents table or --json
driga projects show <project>
driga inbox                       # current waits and recent completions; "inbox list" also works
driga inbox --all                  # complete history, including acks and ended waits
driga inbox ack <item-id>          # explicit acknowledgment
driga conflicts
driga orphans                     # "orphans list" also works
driga orphans kill <candidate-id>  # previews the tree and requires y
driga machines show                     # compact report with 24h trends
driga machines show --full              # all retained trends and process detail
driga machines                     # local + paired machine health
driga agents --machine vm-1
driga agents --watch=2s
```

Hosted sessions (marked hosted or with an origin starting with `hosted`) idle for
more than one hour are hidden from default agent listings, project agent counts,
and summary counts, including the TUI. `agents --all` includes hosted sessions
with activity within the last 24 hours, alongside recently ended sessions.

Collection views merge paired machines by default; `machines show` defaults to local. `--machine all|local|<name|id>`
selects a scope; `--project .` resolves against local repositories only.
View flags are scoped to commands that consume them; see the [command reference](#command-reference).
`--theme=auto|mocha|latte` belongs to `top`; `NO_COLOR` is also honored. View JSON has schema 2,
an observation time, collection mode, source freshness, coverage gaps, and data.
`doctor --json` returns diagnostic checks rather than a view envelope.
Use `driga <command> --help` for command-specific flags.

Labels are `working`, `waiting`, `idle`, `stalled?`, `finished`, `ended`, `dead`, and
`unknown`, with evidence in session details. `stalled?` means suspected inactivity.
`ended` means the process exited with no open turn, a SessionEnd/sessionEnd hook
arrived, or the conversation was replaced by `/clear` or `/resume`. `dead` requires
an interrupted open turn or crash evidence. An unsupported adapter's exit stays
`unknown` when it cannot establish either outcome. `finished` means an unread
completed turn in a continuing session; ending the session preserves unread
completion items in the inbox and closes pending waits. Use `--all` to include
recently ended sessions.

The `ended` label is an additive schema-2 string value, following the
[wire versioning rule](../CONTRIBUTING.md#rules-for-changes). CLI JSON, MCP and
fleet retain schema 2; consumers must tolerate additional label values.
Hosted Codex threads use heuristic labels prefixed with `~`; a shared host process
does not establish individual thread liveness. Unknown agent versions are parsed
tolerantly and reported by `driga doctor`.

`driga top` opens on Home (`0`): what needs you, agent states, machine health, the last
hour's activity, projects and leftovers. On terminals 140 columns or wider the Agents tab
shows the selected agent beside the list. `driga activity` prints the same 60-minute
history (machine samples and events per minute; minutes without a sample are gaps).

In the TUI, use `0`–`8` for tabs (Home, Agents, Projects, Inbox, Orphans, Machine, Cleanup, Flow, Chat), Enter for detail, Esc to return, `?` for help,
`m` to cycle machine scope, and `q` to quit. Viewing details and issuing JSON
queries never acknowledge inbox items. Use `x`/Space in Inbox or `driga inbox ack`. Without a running collector,
acknowledgments are queued for the next collection. Orphan kills recheck process
identity and the descendant set immediately before signaling; young candidates
are refused. `--force` escalates survivors to SIGKILL (TerminateProcess on Windows), with a second confirmation unless `--yes/-y` is supplied.
The Machine tab and web Machine page lead with agents' memory and CPU, session
costs, agent processes without a session, and likely work shared with an editor.
Sessions expand into processes with attribution proofs and their launch app;
Other apps starts collapsed around the existing top-ten memory and CPU lists.
`driga machines show --json` and MCP `get_machine` add the same buckets to schema 2.
Low-disk and sustained critical-memory-pressure warnings open Machine, with
pressure text naming agents' share and observed growth. Open it directly with
`driga top --tab machine`. See [machine health](machine-health.md) for rules,
measurements, JSON fields, and deferred editor integrations.

The default inbox puts current waits first, followed by up to 50 unacknowledged
completions from the last 24 hours. `--since` selects another completion window;
`driga inbox --all` removes the default window/count bound and includes acknowledged
and ended events. Waits for sessions that ended, moved on, or aged out of the
visible agents are labelled **historical wait** in history. Reading or hiding
history never acknowledges it. TUI waiting counts use the same visible session
states as Agents, including waiting sessions without a captured inbox event.
The web inbox has a **show all history** link and pages of at most 100 rows.

Machine text and JSON default to ten rows per evidence list and three nested
process/command/install entries per row. MCP trends read only the last 24 hours;
`trend_since` gives that boundary and `truncated` marks omitted entries. Totals
still include every sampled process. `driga machines show --full` (MCP `get_machine`
with `full: true`) returns all retained trends and uncapped evidence lists.
Schema 2 is retained: field types and the envelope are unchanged; the `machines`
array now contains only additional hosts, because the primary host is already
reported at the top level. A single-host response omits `machines`. Consumers
should read the top-level primary report plus any additional `machines` entries.
Summary reads only the latest health sample and does not build machine trends.

Remote orphans cannot be killed here; run the kill command on their machine.
Remote inbox acknowledgments require the peer's `--allow-ack` grant; unreachable peers
show `ack pending` and retry on reconnect.

Desktop alerts show the session title or project and why it needs you, within
your capture policy. macOS notifications require the installed **Driga.app**
(`brew install --cask driga`); they use Driga's name and icon through the native
UserNotifications API. Allow alerts and sounds for Driga when macOS asks, or in
System Settings → Notifications → Driga. Unbundled builds (`go run`, checkout
binaries) cannot send macOS notifications; `driga doctor` explains how to install
the app. No notification helpers are needed.

Clicking a local item's notification runs `driga inbox jump <item-id>` without
acknowledging it. Jumps select a proven tmux pane, focus a proven Terminal/iTerm
window with `inbox.jump_terminal = true`, or print a resume command. Machine
warnings open Terminal on `driga top --tab machine`. Batch summaries and remote
alerts have no click action. The collector keeps the native notification delegate
and main run loop alive while it runs. Re-sending an item replaces its existing
notification. Linux uses notify-send; Windows uses native WinRT toasts.

Health warnings notify once per condition, until it clears. Concurrent health
warnings from one machine form one summary. Details and cleanup commands remain
in the inbox; low-disk banners show decimal sizes such as “12.2 GB free, below
20 GB”. `inbox.alert_repeat` throttles session alerts, not health episodes.

## Privacy and configuration

The default `capture_text = "titles"` stores generated titles and file paths,
with session/process/Git/machine metadata. Prompt and handoff text is opt-in:
`capture_text = "snippets"` stores at most 200 characters per snippet after secret
scrubbing. Scrubbing cannot guarantee privacy. Full transcripts are not stored.
Agent configuration is only changed by explicit setup/hook commands.

```sh
driga config path
driga config show
driga config edit                  # $EDITOR, falling back to vi
```

To disable desktop alerts:

```toml
[inbox]
alerts = "off"
```

Defaults and their purpose:

| Setting | Default | Purpose |
| --- | --- | --- |
| `ui.theme` | `auto` | Whole TUI palette: `auto`, `mocha`, `latte`; override with `--theme`. |
| `roots` | none | Discover from sessions; add scanning roots only when wanted. |
| `root_depth` | `3` | Bound discovery below configured roots. |
| `exclude` | `**/node_modules`, `**/.tmp/**` | Avoid dependency and temporary directories. |
| `stalled_after` | `10m` | Suspect a quiet busy/mid-turn session after this interval. |
| `orphan_min_age` | `10m` | Show fresh leaks as young; withhold kill until mature. |
| `capture_text` | `titles` | Require opt-in for prompt/handoff snippets. |
| `fleet.serve` | `false` | Open a listener only when explicitly enabled. |
| `fleet.listen` | none | Require an explicit address; fleet's default client port is 7411. |
| `fleet.share_text` | `titles` | Share titles/commit subjects; `none` withholds them. |
| `fleet.enroll_window` | `10m` | Bound pairing; at most three attempts, closes on success. |
| `fleet.allow_any_address` | `false` | Refuse wildcard/public listeners by default. |
| `inbox.alerts` | `waiting` | Desktop alerts for attention; `all` includes completions, `off` disables alerts. |
| `inbox.alert_after` | `0s` | Alert when a wait remains open this long; zero alerts immediately. |
| `inbox.alert_repeat` | `5m` | Minimum interval between alerts for one session; zero disables throttling. |
| `intake.otlp` | `false` | Enable optional metadata-only OTLP/HTTP JSON. |
| `intake.listen` | `127.0.0.1:7418` | Loopback only. |
| `no_service_hint` | `false` | Allow a once-daily service hint in snapshot mode. |
| `summaries.enabled` | `false` | `driga projects summarize` runs only after this opt-in. |
| `summaries.model` | `haiku` | Model passed to `claude -p` for summaries. |
| `cleanup.worktree_idle` | `168h` | Minimum idle time before a worktree is a candidate. |
| `machine.disk_free_min` | `"0"` (automatic) | Inbox `low_disk` warning below the smaller of 10% of the volume or decimal 20 GB; override with a byte string or KiB/MiB/GiB. Env: `DRIGA_MACHINE_DISK_FREE_MIN`. |
| `cleanup.regenerable` | `node_modules/`, `target/`, `.venv/`, … | Ignored directories that may leave with a worktree; extends the defaults. |

The Claude, Codex, Copilot and Gemini adapters are enabled by default. Override them with
`[adapters.<name>]` and `enabled = false`. Gemini CLI sessions are read from
`~/.gemini/tmp/*/chats/` as JSONL transcripts (or under `$GEMINI_CLI_HOME`). Copilot uses hooks and process
observations; without hooks it shows process-level sessions with unknown state.
Install with `driga hooks install copilot`; `driga doctor` reports missing or
stale hooks. Copilot hooks require CLI 1.0.98 or newer. Windows support is experimental. Copilot session-state files are not read. Antigravity (`agy`) keeps a
closed SQLite format, so Driga shows `agy` sessions from the process table only;
`driga doctor` says so. `driga dev agy-schema` prints table and column names and
row counts — never values — from your own `agy` databases, so support can be built
from its output. `open_cmd` optionally configures the TUI's
terminal-opening command using `{cwd}`; otherwise the terminal is detected.

| Data | Default path | Override |
| --- | --- | --- |
| SQLite database and collector state (macOS) | `~/Library/Application Support/driga/` | `DRIGA_DATA_DIR` |
| SQLite database and collector state (Linux) | `$XDG_DATA_HOME/driga`, else `~/.local/share/driga/` | `DRIGA_DATA_DIR` |
| Hook/ack spool (macOS) | `~/Library/Application Support/driga/events/` | `DRIGA_SPOOL_DIR` |
| Hook/ack spool (Linux) | `$XDG_STATE_HOME/driga/events`, else `~/.local/state/driga/events/` | `DRIGA_SPOOL_DIR` |
| SQLite database and collector state (Windows) | `%LOCALAPPDATA%\driga\` | `DRIGA_DATA_DIR` |
| Hook/ack spool (Windows) | `%LOCALAPPDATA%\driga\events` | `DRIGA_SPOOL_DIR` |
| Configuration (Windows) | `%APPDATA%\driga\config.toml` | `DRIGA_CONFIG` |
| Configuration (macOS/Linux) | `$XDG_CONFIG_HOME/driga/config.toml`, else `~/.config/driga/config.toml` | `DRIGA_CONFIG` |

Data, spool and config paths (including platform-derived defaults) are expanded,
cleaned and resolved to absolute paths once at startup. Relative overrides such
as `DRIGA_DATA_DIR=a/data` are relative to the startup working directory.

Database/spool files use mode `0600`, and their directories use `0700`. Claude
data is read under `~/.claude/`; Codex under `~/.codex/`, or `CODEX_HOME` when set.
The Codex catalog is opened read-only. Local Git commands never fetch.

MCP trend storage interns command/host text and stores state changes plus hourly
heartbeats. It keeps full minute-observation detail for 24 hours, then hourly
last-state endpoints to 30 days; `driga machines show --full` labels the retained
resolution. Unchanged observations update an endpoint without adding a state
row. Default reports and critical-pressure alerts read at most 24 hours.

Pre-release databases must be reset; `driga doctor` prints the exact stop/remove/start command.
Fresh databases enable incremental auto-vacuum before creating tables. The
collector returns up to 256 free pages after each hourly retention pass.
Long-lived read snapshots can delay WAL space recovery until they close.

`driga doctor` warns when the database plus WAL exceeds the **500 MB**
(500,000,000 byte) budget and lists the largest table, including its indexes.
The budget is a diagnostic threshold; actual size depends on group turnover
and observed state changes.

After pairing, the serving peer controls what leaves that machine:

| Data | Shared by default? | Control / limit |
| --- | --- | --- |
| Host/OS/resources, session labels/evidence/timestamps, cwd and touched file paths | Yes | Metadata; paths can contain identifying text. |
| Session titles and Git commit subjects | Yes | `fleet.share_text = "titles"` is the default; use `"none"` to withhold. |
| Prompts, handoffs, textual inbox summaries | No | Requires peer `fleet.share_text = "snippets"` **and** local `capture_text = "snippets"`; scrubbed, at most 200 characters. Otherwise inbox summaries are generic. |
| Repository/worktree paths, branches, dirty counts, remote URLs | Yes | URL credentials stripped. |
| Processes and orphan candidates | Agent processes only | Command lines scrubbed and truncated to 200 characters. |
| Machine samples, source freshness, coverage gaps | Yes | Same retention and private file permissions on the viewer. |
| Full transcripts, API keys | No | Full transcripts are not stored; scrubbing cannot guarantee every secret is removed from metadata/snippets. |
| OTLP prompt/response/tool input/bodies, `user.*`, `organization.id` | No | Dropped before storage. |

A viewer stores the text a peer elects to share, even if the viewer's own local
capture setting is `titles`. Pair only with viewers you trust to retain metadata.
Fleet trust files live under the data directory's `fleet/` (directory `0700`,
files `0600`); do not copy its identity key between machines.

## Hooks and service

Preview optional integration before applying it:

```sh
driga hooks install --dry-run
driga hooks install
driga hooks status
driga hooks uninstall --dry-run
driga hooks uninstall
```

A positional adapter name (`claude|codex|copilot|all`) selects the integration.
Installation shows a line-level unified diff with three context lines and `@@`
hunk ranges, then asks for confirmation. Existing Claude and Codex
files are backed up to `*.driga-bak-<timestamp>`.
Claude installation appends commands for Stop, Notification, SessionStart,
SessionEnd and PermissionRequest to `~/.claude/settings.json`, preserving existing
hooks. Codex
installation manages only the `[[hooks.PermissionRequest]]` block in
`config.toml`, preserving comments and existing `notify` settings. Uninstall
removes only Driga entries. Capture hooks append events to the spool; the collector
alone writes SQLite. Claude PermissionRequest uses the in-memory broker instead.

Copilot installation manages only `$COPILOT_HOME/hooks/driga.json`, falling back
to `~/.copilot/hooks/driga.json`. It captures sessionStart, userPromptSubmitted,
postToolUse, postToolUseFailure, notification, agentStop, errorOccurred, subagentStart,
subagentStop and sessionEnd. Each hook has a 3-second timeout; normal awaited
capture targets a 50 ms warm-machine budget.
Permission prompts and elicitation dialogs enter the needs-you inbox; other
notifications do not. Capture prints nothing and always exits successfully.
Uninstall deletes only this file. Running Copilot sessions keep calling the
removed hook until restarted; Copilot fails open. Copilot reloads hooks at CLI startup.
Jumps use the linked process's TTY, with `copilot --resume=<sessionId>` as fallback.
Under `capture_text = "titles"`, Copilot uses the project name as its session title
and captures tool names. Only `snippets` allows bounded, scrubbed prompts, prompt
titles, tool details and notification messages. Permission waits carry no tool name.
Background notifications never update activity. Waits end on the same session's
prompt, successful or failed tool result, agentStop, errorOccurred or sessionEnd.
Session start records lifecycle metadata without resetting an open turn or wait.
Errors finish the turn; a process gone after a fatal error is `dead`. Subsequent
turn activity resumes a recoverable error. A normal sessionEnd is `ended`.
Only `snippets` stores a scrubbed `error: <name>` reason; error messages and stacks
are never stored.

Only a parent matching a native Copilot executable or its npm loader is linked.
Other hook sessions stay live by hook activity for up to 24 hours. VS Code's
snake_case hook payloads are tagged `vscode` and never linked to a PID. Large
`toolResult` fields are streamed and discarded, with 1 MiB retained metadata and
64 MiB total stdin limits. Doctor warns about older CLI versions, enabled Copilot
sandboxing and Copilot processes with no received hook events.

The service is a per-user LaunchAgent on macOS and a systemd user service on
Linux, named `run.driga.collector`:

```sh
driga service status
driga service logs -f
driga service stop
driga service start
driga service restart
driga service uninstall
```

### Service identity

- **macOS:** release binaries are signed by **LabStack LLC**. The Background
  item shows the developer name; the launchd label is `run.driga.collector`.
- **Linux:** the systemd user unit displays **Driga collector**.
- **Windows:** Task Scheduler lists **Driga collector** under
  `\Driga\<user SID>`. Task Manager shows **Driga** with the Murmur icon.

On macOS logs are at `~/Library/Logs/driga/collector.log`; Linux uses
`journalctl --user -u run.driga.collector` (`driga service logs -f` wraps it).
On a Linux VM, allow the user service to run after logout:

```sh
driga service install
loginctl enable-linger "$USER"
driga doctor
```

The linger command may require administrator authorization. Linux without
systemd can run `driga collect` under tmux/nohup instead. Missing PSI in containers
is supported and shown as `psi n/a`; memory pressure uses MemAvailable/MemTotal.

`driga collect` runs a foreground collector; `driga collect --once` writes one collection pass. Only one collector
can hold the lock in a data directory. Idle collection samples processes/machine
every 60 seconds and Git every five minutes. A viewer heartbeat speeds these to
5 seconds, 10 seconds, and 30 seconds respectively. Cwd lookup remains at most
once per minute. Full Git status runs at least every five minutes.
To see where a collector spends CPU, stop the service and run
`driga collect --cpu-profile cpu.pprof` (it records for `--profile-for`, default 2m),
then read it with `go tool pprof -top $(which driga) cpu.pprof`. When the window
ends or the collector stops early, it also writes an allocation profile to
`cpu.pprof.alloc`; inspect it with
`go tool pprof -sample_index=alloc_space -top $(which driga) cpu.pprof.alloc`.

## Windows (experimental)

Windows 10 1809+ / Windows 11 support the same read interfaces, foreground
collector, optional OTLP intake, MCP, web and fleet. Phase 3 is feature-complete
pending real-hardware verification; Windows remains experimental.

`driga orphans kill <id>` and the TUI action use the
same driga-proven orphan candidates and confirmations as Unix. Windows orphans
have a missing parent or a parent whose start time proves PID reuse; Windows
does not reparent them to init or a Linux subreaper. Every tree member's kernel
start time is pinned in the stored candidate and checked again before action.
Changed trees abort, including changes during the grace period or before force.
Old candidates without member identities require collection and listing again.

For observed orphan trees, the graceful step posts `WM_CLOSE` only to that PID's
top-level windows. Process snapshots cannot prove console-group membership, so
Driga never attaches to an arbitrary console or broadcasts a control event.
After the usual five-second grace, `--force` uses `TerminateProcess` through an
identity-checked handle, with the same second confirmation as Unix. Headless
processes without windows may need `--force`. Children launched by Driga (Chat,
LLM probes and collector Git probes) enter a non-breakaway, kill-on-close job
before their initial thread runs. On cancellation, a driga-created console
group sharing its console can safely receive `CTRL_BREAK`; otherwise Driga
tries `WM_CLOSE`. Closing the job also ends any remaining descendants.

Socket evidence uses `GetExtendedTcpTable` (`TCP_TABLE_OWNER_PID_ALL`) and
`GetExtendedUdpTable`, for IPv4 and IPv6. TCP listeners/connections and bound
UDP endpoints prevent an idle verdict; table errors remain unknown. UDP owner
tables do not expose peers, so a bound endpoint is conservatively treated as a
listener. Open-file evidence uses the unprivileged Restart Manager API, without
WMI or PowerShell. Driga walks regular files below the directory, including use
by its own processes, and registers batches of 100 files. The walk is capped at
10,000 entries with a five-second context budget (or the caller's earlier deadline). Unreadable entries, non-regular files, exceeded limits
and API errors leave evidence unknown. Native calls cannot be interrupted; a call
that finishes past the deadline also leaves evidence unknown.
Directory handles are not Restart Manager resources; process cwd checks remain
separate. See [Restart Manager resources](https://learn.microsoft.com/en-us/windows/win32/api/restartmanager/nf-restartmanager-rmregisterresources).

Cleanup retains the same candidate checks, preview, confirmation, ignored-file
review and `cleanup.log` audit trail. On Windows, verified linked worktrees go
to the Recycle Bin through `IFileOperation` on a COM STA, with recycling, undo
and silent/no-error-UI flags. The operation vetoes permanent deletion, checks
the Shell's per-item recycling result, then prunes Git worktree metadata and
optionally deletes a merged branch. Restore files through the Recycle Bin;
Git metadata/branch restoration is separate. Agent history remains report-only.
Claude scratch discovery and cleanup stay refused: repository fixtures do not
establish Claude's Windows scratch layout. `doctor` explains this fail-closed
limit. No Unix temp layout is assumed.

The `Windows end-to-end` CI job runs on GitHub-hosted `windows-2025` machines.
It builds `driga.exe` and drives subprocesses with disposable homes and synthetic
Claude fixtures: snapshot attribution, foreground collection, the Task Scheduler
lifecycle, dashboard/static assets and CSP, installed hooks through `cmd.exe`,
confirmed orphan kill, and linked-worktree recycling with Git metadata cleanup.
Notification availability is checked through `doctor`; toast display is not sent.
Run it locally with `go test -tags e2e ./internal/e2e -v` on Windows; it requires
Git, Task Scheduler, an unused Driga task for the current user and a local volume
supporting the Recycle Bin. Tests only signal their own processes. The Windows
platform tests also verify Restart Manager against a child holding an open file.

Real-hardware checks still include real agent attribution, logon/restart, notifications,
console/window shutdown, job descendants, socket ownership and Recycle Bin restore.
The native API contracts follow Microsoft's
[console group rules](https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent),
[IP Helper tables](https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getextendedtcptable)
and [IFileOperation flags](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-ifileoperation-setoperationflags).

`driga service install` registers `\Driga\<current-user-SID>\Driga collector`
and starts it. The task runs `driga.exe collect` with internal scheduler flags,
at that user's logon, using the interactive token and least privilege. It has
no password, elevation or PowerShell dependency. Failure restarts are attempted
every minute, up to 999 times; battery operation and unlimited runtime are enabled.
It stops at logout. `service stop` ends the task through Task Scheduler; SQLite
recovers normally on restart. `restart` waits for the prior task to stop.
Installation refuses an existing task; uninstall before changing its binary path.
`service status` shows the task state plus collector lock/PID liveness; a running
task alone does not prove fresh collection. Check `doctor` for freshness.
The scheduled collector detaches its console immediately; verify whether the
host briefly shows a console during task launch. It uses the logon environment
plus the home, temporary-directory, Driga config/data/spool and agent-path
overrides recorded at installation. Reinstall after changing those paths. Other
variables, including PATH, come from the logon environment.

Collector logs are at `%LOCALAPPDATA%\driga\logs\collector.log` (or
`DRIGA_DATA_DIR\logs\collector.log`), bounded to 10 MiB plus one rotated file.
`driga service logs [-f]` reads them directly. Agent homes use `%USERPROFILE%`
and their usual overrides. Windows private files/directories use a protected
DACL for the current user, SYSTEM and administrators.

Hook setup previews the diff and preserves the same owned-file rules and edit
checks as Unix. Claude uses the documented executable/`args` form on Windows:
[Claude hook reference](https://code.claude.com/docs/en/hooks#command-hook-fields).
The executable is an absolute JSON string; shell quotes are not part of an exec
filename. This avoids Git Bash/PowerShell selection and path expansion. Use a
Claude version supporting exec-form hooks. Copilot similarly uses absolute
`exec` plus `args`. Codex's permission hook uses
`cmd.exe /d /c call "<absolute driga.exe>" hook codex PermissionRequest`, valid
from cmd.exe and PowerShell. This conservative string form rejects shell
metacharacters in the install path (including `%`, `!`, `&`, parentheses, `$`
and backticks); choose a plain stable directory. Spaces, apostrophes and Unicode
are supported. `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and `COPILOT_HOME` are respected.

Alerts use WinRT with a per-user `LabStack.Driga` notification identity, registered
on the first alert. No PowerShell or cgo is used. Toasts are plain, without click
activation; use `driga inbox` and `driga inbox jump <item>` for navigation. Windows
notification policy, Focus Assist/Do Not Disturb, and interactive desktop state
can suppress delivery. Doctor probes availability without registering or sending;
it reports `notifications unavailable` for native API/setting failures.

For local session jumps and the TUI open action, Windows Terminal is preferred
(`wt.exe -d <cwd>`); otherwise Driga uses
`cmd.exe /c start "" /D <cwd> cmd.exe`. Directories are quoted for cmd.exe.
This opens a new terminal; stored agent resume commands are shown for copying,
never executed. Existing terminal focus by PID is deferred because ownership of
a terminal window is not proven by the current process data. Remote cached jumps
are never executed locally.

## Fleet setup

Each serving machine runs a collector; each viewer's collector pulls its paired
peers. Pairing is directional: repeat in the other direction if both machines
should see each other. Use an explicitly configured private LAN or overlay
address (for example, a VPN interface); no particular overlay is required.

On the peer, stop an existing collector before starting a foreground server:

```sh
driga service stop                 # if installed/running
driga collect --serve --listen <private-ip>:7411
```

For persistent serving, put this in the peer's configuration and restart its
service (or run `driga collect`):

```toml
[fleet]
serve = true
listen = "<private-ip>:7411"  # replace with the peer's actual private address
share_text = "titles"
```

In another peer terminal, open enrollment. On the viewer, add the peer, compare
the six-digit code displayed by **both terminals**, and answer `y` on each:

```sh
# Peer terminal:
driga fleet enroll --allow-ack           # omit --allow-ack for read-only access
# Viewer terminal:
driga fleet peers add <private-ip>:7411 --name vm-1
driga service install             # or start an existing service / driga collect
driga machines
driga fleet peers list
driga agents --machine vm-1
```

`fleet peers add` reports the actual grant: `read ✓ · ack ✓` or `read ✓ · ack ✗`.
Enrollment lasts at most ten minutes, allows at most three attempts, and closes
on success. For headless pairing, exchange full device IDs over a trusted channel:

```sh
# On each machine, obtain its own ID:
driga fleet id
# Peer: authorize exactly the viewer's ID; this returns with the window open.
driga fleet enroll --allow-viewer <viewer-device-id> --allow-ack
# Viewer: require exactly the peer's ID; neither side prompts.
driga fleet peers add <private-ip>:7411 --name vm-1 --device-id <peer-device-id>
```

Both `--allow-viewer` and `--device-id` are needed to make both sides headless. Keep host firewall
and cloud security-group ingress restricted to viewers on the private network
(TCP 7411, or your configured port); allow viewer egress to that port. A private
address alone does not open a firewall. Wildcard/public listeners are refused
unless `fleet.allow_any_address = true` is explicitly configured. Mutual TLS 1.3
pins each certificate; an identity change becomes `untrusted` and requires
removal and deliberate re-pairing. No automatic trust on reconnect.

For a bastion-only host, listen on the peer's loopback and tunnel from the viewer:

```sh
# Peer, in place of the private listener:
driga collect --serve --listen 127.0.0.1:7411
# Viewer, keep this SSH connection running (optionally add -J <bastion>):
ssh -N -L 7411:127.0.0.1:7411 <host>
# Pair through the tunnel while enrollment is open on the peer:
driga fleet peers add 127.0.0.1:7411 --name vm-1
```

If local port 7411 is busy, use another local port in both `-L` and `fleet peers add`.
The pinned peer certificate authenticates the endpoint through the tunnel; no
public fleet port or inbound security-group rule is needed for loopback serving.
An interrupted tunnel shows the peer as stale and reconnects when it returns.

`driga fleet status` shows serving streams and enrollment;
`driga fleet viewers list` lists authorized viewers. `driga fleet viewers revoke <viewer>`
revokes access immediately. `driga fleet peers remove <name>` stops
pulling locally and keeps cached history; add `--purge` to delete cached rows.
You can also run `driga fleet peers remove <name-or-machine-id> --purge` after a peer
has already been removed. Ambiguous removed-peer names require the machine ID.
Stopped/unreachable peers
become stale after 45 seconds without contact; last-known rows remain visible.
Epoch/retention changes trigger a new snapshot. Clock skew over 60 seconds warns
in `driga machines`.

Before enrolling a cloned VM, stop its collector and run
`driga machines reset-id` **on the clone**, then restart and re-pair. This creates a
new machine ID and certificate and invalidates old pairings. `driga fleet rekey`
rotates only the certificate, also requiring a stopped collector and re-pairing.
On a viewer that reports the rotated peer as untrusted, deliberately remove the
peer, reopen enrollment on the serving peer, then add it again using its new
device ID (or compare pairing codes). Purging is unnecessary: retained history
from the removed certificate does not block re-pairing. Two active peers claiming
the same machine ID still require `machines reset-id` on the clone.

`driga fleet peers list`, `driga machines` and `driga doctor` show the last fleet
failure, including bounded, scrubbed server diagnostics such as snapshot errors.
Doctor also reports the actual Chat and fleet control socket paths. Both sockets
prefer their data-directory paths (`chat.sock` and `fleet/fleet.sock`). If a path
exceeds 100 bytes, they use stable, separate names keyed by the absolute data
directory under the OS temporary directory (`TMPDIR` on Unix, `TEMP`/`TMP` on
Windows), in a private per-user `driga-<user>` directory. If that is also too long,
Unix falls back to `/tmp/driga-<uid>`; Windows tries the short TEMP name, then
`%SystemRoot%\Temp\driga-<SID-hash>`. This byte limit fits both Unix and Windows
AF_UNIX limits. Directories must be real, owned by the current user and mode
`0700` on Unix; Windows uses a private owner DACL. Unix sockets use mode `0600`.
Chat is optional: failure to create its socket logs a warning and appears in
doctor while collecting and serving continue. Fix the reported path or
permissions and restart the collector to retry Chat.

## Cleanup

`driga cleanup list` lists disk that agents left behind. Nothing is removed
automatically, and nothing is removed on another machine.

```sh
driga cleanup list                        # candidates with evidence
driga cleanup list --sizes                # walk sizes on demand (30 s cap per walk)
driga cleanup list --why                  # also list worktrees that don't qualify, and why
driga cleanup remove <id> --dry-run        # print the exact commands
driga cleanup remove <id>                  # re-verify, then ask y/N
driga cleanup remove --kind worktree --all # every ready worktree; skips anything changed
```

- **Worktrees**: a linked worktree (never the main or a locked one) with no live
  session or process inside, no tracked or untracked changes, commits merged
  into the default branch or pushed, and idle for `cleanup.worktree_idle`. The
  action is `git worktree remove`, `git worktree prune`, then `git branch -d`
  only when merged (never `-D`). Ignored build output (`cleanup.regenerable`) leaves
  with the worktree; any other ignored file (`.env`, local databases) makes the
  candidate `needs-review`, and acting on it needs `--include-ignored`.
- **Scratch**: a Claude Code per-session temp directory whose session has ended
  for 24 h and has no open files. It is moved to the trash (macOS Trash,
  or the freedesktop trash on Linux), never deleted.
- **History**: agent history sizes by age. Report only; use the agent's own
  retention setting (Claude Code: `cleanupPeriodDays`).

Every check runs again just before acting; if anything changed, the action is
refused with the reason. Each attempt is appended to `cleanup.log` in the data
directory; if that write fails after the action succeeded, the action reports a
warning instead of an error. The TUI has a Cleanup tab, and `list_cleanup_candidates` is a
read-only MCP tool.

## Summaries and Chat

Both run your installed `claude` CLI, on demand only; Driga never calls a model in
the background and has no API key.

```sh
driga projects summarize <project>... | --all   # needs [summaries] enabled = true
driga ask "which agents are waiting on me?"
```

`summarize` sends the project name, branches, dirty/ahead/behind counts, the last
five commit subjects, recent session titles and — only with
`capture_text = "snippets"` — handoff text. It never sends transcript lines, file
contents or paths. It runs `claude -p --safe-mode --no-session-persistence --tools ""`
with a $0.05 budget cap. A summary built from handoff text is shared with fleet
viewers only at `fleet.share_text = "snippets"`.

`driga ask` is a one-shot Chat broker client. Chat uses your installed Claude CLI
and login; Codex and Copilot are listed as coming. Required flags are probed on
the first turn, and an unsupported CLI fails with the missing flag and version.
No real agent CLI is run by tests.

Open **8:chat** or press **c** on any web console page to dock Chat on the right.
Non-command text in the command bar opens the composer. Enter sends and
Shift+Enter adds a line; use Stop to cancel the current turn and queued messages,
and use History to reopen a conversation. Escape closes the panel when the input
is empty; with a draft, the first Escape blurs the input and keeps the draft.
Context chips describe the current page, selected session/project and machine;
remove a chip to leave it out.
Session references in answers select the session on the current page. Tool calls
appear in a collapsed **Tools used** list. Driga replies are rendered as Markdown
and marked **answered by Claude Code**. A timed indicator shows thinking, writing
or the current read tool. Streaming follows the bottom until you scroll up; reduced
motion uses a static dot.

In `driga top`, press **8** or **c** for Chat with the current selection. Enter
sends; Shift+Enter adds a line, or Alt+Enter in terminals that cannot report
Shift+Enter. Escape stops an active reply; otherwise
it enters **navigate** mode and keeps the draft. In navigate mode, normal keys
work: **0–8** switch tabs, **q** quits, **?** opens help and **/** filters. Press
**c** or **Enter** to type again. **Alt+0–Alt+8** switch tabs from either mode;
returning to Chat restores the draft. The footer shows the current mode.
Help scrolls with arrows, PgUp/PgDown or the mouse wheel. Applied list filters
stay visible in the status line; Escape clears the filter when no overlay is open.
Session details load when selected, rather than for every overview row.
PgUp/PgDown scroll the visible transcript with a one-line overlap and stop at its ends. Alt+Up recalls the last draft, Ctrl+H cycles history, Ctrl+N
starts a conversation, Ctrl+T toggles tool details, Ctrl+X clears context and
Ctrl+0 returns Home. Thinking/writing/tool status includes elapsed time and a
streaming cursor; NoColor uses a static indicator. Flow's copy-resume key is **C**.

Chat can acknowledge inbox items, jump to a session terminal, kill an agent or
orphan, clean up a candidate and refresh project summaries. It also has Claude’s
built-in shell and file tools. Actions are local-only and keep the CLI’s process,
cleanup and jump safety checks. Chat never types into a live agent terminal.

Each conversation starts in **Confirm**. Change trust mode with the web header
buttons or **m** in TUI navigate mode:

| Mode | Runs without asking | Asks |
|---|---|---|
| Confirm | Driga read tools and reads inside the working directory | Reads elsewhere and every other call |
| Auto | Auto allows driga read tools, reads inside the working directory, inbox acknowledgments, jumps and summaries, and allows Edit/Write only inside a git project working directory that is neither home, the driga data directory nor an ancestor of either, with no hidden components or agent instruction names (`claude*.md`, `agents*.md`, `gemini*.md`, `copilot-instructions.md`, case-insensitive) in the given or resolved relative path after trimming trailing dots and spaces, and always asks on colons. | Everything else asks |
| Full auto | Everything | Nothing |

Full auto requires confirmation and is shown in red with its remaining time.
The TUI warning stays visible while replies and action cards update; press **m**
again to confirm, or another key to cancel. Full auto lasts at most 30 minutes. It ends when you switch it off, close the conversation
or restart the broker; it is never persisted. Only clients can change modes or
decide cards. Model text, files and tool output cannot change that authority.

An action card shows the tool, exact input, working directory, status, who decided
and the result. Click **Allow** or **Deny** on the web. In TUI navigate mode,
**y** allows and **n** denies the oldest pending card; while typing use **Alt+y**
and **Alt+n**. The status line says **action waiting**. Stop denies pending cards
before stopping the turn and queue. Running allowed actions show
**stopped · result unknown** until a later result arrives. Unanswered cards
expire after ten minutes.
Action log entries follow the conversation capture policy and retention. Tool,
status and deciding actor survive a restart even when message bodies are not
captured. Completed cards leave the bounded live buffer as new actions arrive;
pending and running actions remain available until decided or completed.

The explicit allowlist includes driga’s read tools and TodoWrite. Read, Glob,
Grep, LS and NotebookRead are not pre-allowed: reads inside the working directory
run; reads elsewhere ask in Confirm and Auto. This also applies to read-only
Bash commands such as `head /etc/hosts`. Full auto runs those reads without a
card. Other calls go through driga’s permission gate.
Chat uses `--tools default`, `--setting-sources ""` and `--strict-mcp-config` to
keep user settings, hooks, plugins and other MCP servers out. If Claude lacks
`--permission-prompts`, Chat stays read-only and explains why once per conversation.

`driga ask` stays read-only: it has no action cards. The ordinary `driga mcp`
server for your own agents also stays read-only. Chat’s private `mcp --chat`
child supplies its action tools and permission bridge.

The collector owns the broker and its local socket, using the shared directory
rule described in Fleet setup. Web, TUI and ask connect locally. Without a service, the opening
client runs a private in-process broker and labels it **conversations end with it**.
No driga conversation history survives that client. With the service, conversation
IDs, vendor resume IDs and capture-filtered messages persist in SQLite for 30 days;
interrupted work is marked interrupted after restart and is never replayed to the
agent automatically. SSE cursors replay bounded ordered events; an expired cursor
or service restart sends a replacement snapshot. Existing web and TUI clients
reload the local broker key after a collector restart. Chat streams wake on broker
events and retain a 15-second heartbeat. If the web process itself restarts, reopen
its newly printed authentication URL, then reload any disconnected tab.

Your explicitly submitted question is scrubbed and sent to Claude. Context and
MCP evidence pass the capture policy before reaching the agent. Driga retains no
chat message bodies under `capture_text = "titles"`; `snippets` retains scrubbed
200-character snippets. Live conversation text and drafts are bounded in memory,
and browser history stores no chat text on disk. Tool names and safe action decision metadata are retained as
metadata. Process command lines and argv require `capture_text = "snippets"` in
Chat tool responses; other policies remove both argument-bearing fields. Downgrading to titles erases retained bodies when the broker starts.
Claude's own session history is vendor-managed and enables `--resume`; driga's
capture policy does not control that vendor history. Chat tools may make network connections when approved; Claude uses its existing
vendor connection.

## Web dashboard

`driga web` serves an interactive dashboard for the same data in your browser:

```sh
driga web            # prints a one-time URL such as http://127.0.0.1:53817/?token=…
driga web --open     # also opens it
```

It listens on loopback only (`--listen` refuses other addresses), runs as its own
process so the collector's footprint is unchanged, and updates live as the collector
writes. The token in the printed URL sets a cookie for that browser; restarting
`driga web` issues a new token. Pages: Home, Agents, Projects, Inbox, Orphans,
Machines, Cleanup (listing only), Flow and Chat. Chat provides local actions through approval cards.

## MCP registration

`driga mcp` exposes read-only tools over stdio with the same schema-2 view
envelopes as `--json`. Reading never acknowledges inbox items. Run a collector for
history and merged fleet data; without one, tools use labelled snapshot mode.
Keep the installed `driga` binary on the MCP client's PATH.

Claude Code registration:

```sh
claude mcp add --scope user driga -- driga mcp
```

In the Codex configuration (`~/.codex/config.toml`, or `$CODEX_HOME/config.toml`):

```toml
[mcp_servers.driga]
command = "driga"
args = ["mcp"]
```

Select local data with `machine: "local"` in each tool request. MCP startup accepts
`--snapshot` for one-shot reads; machine/project/time scopes belong to tool arguments.

## Optional OTLP intake

Enable the collector's loopback receiver in Driga configuration, then restart the
collector:

```toml
[intake]
otlp = true
listen = "127.0.0.1:7418"
```

Preview and apply exporter configuration separately from command hooks:

```sh
driga hooks install --otlp claude --dry-run
driga hooks install --otlp claude
driga hooks status --otlp
# Optional Claude beta spans: add --traces to install.
```

For Claude, this edits the settings `env` block: telemetry is enabled, logs use
OTLP, metrics and (by default) traces are disabled, protocol is `http/json`, and
the endpoint is `http://127.0.0.1:7418`. It does not enable prompt/tool-content
logging. `--traces` enables Claude spans. For Codex (`codex`), it adds
`[otel]` with a JSON HTTP logs exporter to `/v1/logs` and `log_user_prompt = false`;
Codex facts remain unmapped pending a scrubbed capture fixture. The default
`all` configures both. Installation refuses existing telemetry settings,
previews changes, asks for confirmation, and backs up edited files. It configures
exporters instead of installing hooks; existing hooks are left in place.

The intake accepts OTLP/HTTP JSON (including gzip); protobuf returns 415. Content
attributes are discarded before storage. It only enriches existing sessions;
unmatched session IDs are rejected with partial-success counts and never create
sessions. A rejected export still returns HTTP 200, with `partialSuccess`, a
string-encoded `rejectedLogRecords` (or `rejectedSpans`) count and `errorMessage`.
Metrics are not used: their data points are rejected with `rejectedDataPoints`
and the reason `metrics are not used`. Empty exports return `{}`.
Use `driga doctor` for received, accepted/matched, rejected/unmatched and unused
metric data point counts, alongside HTTP request failures and content drops.

To undo, preview then remove Driga's intact exporter settings, disable
`intake.otlp`, and restart the collector:

```sh
driga hooks uninstall --otlp claude --dry-run
driga hooks uninstall --otlp claude
# Repeat with codex if installed there (or use all).
```

Uninstall preserves unrelated settings and leaves modified exporter blocks alone.

## Extend process signatures

Agent/helper patterns live in
[`internal/signatures/signatures.toml`](../internal/signatures/signatures.toml).
Add an extra TOML file and list it in configuration:

```toml
[orphans]
extra_signatures = ["~/extra-agent-signatures.toml"]
```

An illustrative signature file:

```toml
[[signature]]
name = "example-helper"
agent = "codex"
role = "helper"
match = ['^example-agent-helper([[:space:]]|$)']
```

Extra files are read in configuration order before built-in signatures; first
match wins. Roles are `interactive`, `headless`, `host`, `helper`, and `app`.
Match executable tokens specifically. A match contributes helper evidence;
process ancestry, reparenting, activity, and running app ownership also matter.


### Themes

Set `[ui] theme = 'auto'` (the default), `'mocha'`, or `'latte'` in TOML.
For example:

```toml
[ui]
theme = 'latte'
```

`driga top --theme mocha` overrides the config for the whole TUI. Auto follows
terminal background detection; `--no-color` keeps the layout without ANSI styling.
The web console's Mocha, Latte, and System choices use the same Catppuccin tokens,
including assistant Markdown. Web System follows the browser's color preference.

### Answer Claude permission prompts

With the collector running, `driga hooks install claude` offers a settings diff
that includes `PermissionRequest` with a 660-second timeout. Repair updates it;
uninstall removes it. Restart existing Claude sessions after changing hooks.

Pending local Claude prompts appear above the inbox waits with the session,
tool, and exact input. In the TUI select a prompt and press `y` to allow or `n`
to deny (`a` remains attach); Enter opens the full input. In the web inbox use
Allow or Deny. Browser decisions require the authenticated session, Origin and
CSRF token.

Action-enabled Chat's `list_prompts` shows inputs only with `capture_text = "snippets"`;
other capture policies show tool metadata. `answer_prompt(id, allow)` uses the
same action gate as other Chat actions: Confirm and Auto require a card;
Full auto runs it immediately. The standalone `driga mcp` stays read-only.

Prompts live in memory only, never in the database, logs or fleet. Claude
permission notifications retain only the tool and needs-you metadata, even with
snippets capture. First answer
wins. A matching transcript tool result retires a terminal-answered prompt within
about a second; session end, hook exit and ten minutes also retire it. Any error,
timeout or absent collector exits the hook successfully without output, leaving
the terminal to decide. Denials say “Denied from driga”. Driga never creates
“always allow” rules or emits `updatedPermissions`. Codex, Copilot, Gemini and
paired-machine prompts are outside this feature.


## Command reference

`driga` defaults to `summary`. Resource groups default to `list`: `agents`,
`projects`, `inbox`, `orphans`, `machines`, and `cleanup`. Fleet peer and viewer
commands require an explicit verb. Session IDs are numeric stored execution IDs;
an adapter identifies Claude, Codex, Copilot, or another integration. A family
contains related sessions; selecting a session never selects the whole family.

| Command | Purpose and options |
| --- | --- |
| `summary` | One-screen summary; supports machine, project, since, snapshot, watch, JSON and color options. |
| `flow [<session-id>]` | Session-family flow; `--minutes=15\|60\|240`. Omit ID to select the newest live interactive session; `--since` limits that selection. |
| `activity` | Last hour of activity; supports machine, project, snapshot, watch, JSON and color options. |
| `agents list` | Sessions grouped by family; `--all` includes sessions ended within 24 hours. Supports read-view options. |
| `agents show <session-id>` | One session; supports machine, project, snapshot, watch, JSON and color options. |
| `agents jump <session-id>` | Confirm and jump to one local session; `--yes/-y` skips confirmation. |
| `agents kill <session-id>` | Confirm and terminate one local idle session; `--force`, `--yes/-y`. Identity and idle checks always apply. |
| `projects list` | Project status; `--active`, `--at-risk`, and read-view options. |
| `projects show <project>` | Project status, worktrees and sessions; `--machine`, `--snapshot`, `--watch`, `--json`, `--no-color`. |
| `projects summarize <project>...` or `projects summarize --all` | Refresh opt-in local summaries through Claude; `--json`. Explicit projects and `--all` are mutually exclusive. |
| `inbox list` | Attention and completion items; `--all` includes acknowledged/history items. Supports read-view options. |
| `inbox ack <item-id>` or `inbox ack --all` | Explicit acknowledgment; `--machine` selects the source. |
| `inbox jump <item-id>` | Jump to a local inbox destination without acknowledgment. |
| `conflicts` | Current/recent overlaps; supports read-view options. |
| `orphans list` | Orphan candidates and evidence; supports read-view options. |
| `orphans kill <orphan-id>` or `orphans kill --all` | `--all`: Select all local orphan candidates; verify and confirm each unless --yes. Supports `--machine`, `--snapshot`, `--force`, `--yes/-y`. |
| `machines list` | Known machine connection health; `--machine`, `--snapshot`, `--watch`, `--json`, `--no-color`. |
| `machines show [<machine>]` | Default target is `local`; `all` is rejected. `--full`, `--project`, `--snapshot`, `--watch`, `--json`, `--no-color`. |
| `machines reset-id` | Confirm local cloned-machine identity repair; `--yes/-y`. |
| `cleanup list` | Local candidates; `--kind=worktree\|scratch\|history`, `--sizes`, `--why`, `--json`. History is report-only. |
| `cleanup remove <candidate-id>` | Verify and remove one candidate; `--dry-run`, `--sizes`, `--include-ignored`, `--yes/-y`, `--json`. |
| `cleanup remove --all --kind worktree` or `--kind scratch` | Remove ready candidates of the selected kind; report completed/skipped/failed counts. `--include-ignored` requires one explicit ID. |
| `top` | Interactive terminal dashboard; `--tab=home\|agents\|projects\|inbox\|orphans\|machine\|cleanup\|flow\|chat`, `--theme=auto\|mocha\|latte`, `--machine`, `--project`, `--since`, `--snapshot`, `--no-color`. |
| `web` | Serve the interactive browser dashboard on loopback; `--listen`, `--open`, `--machine`, `--project`, `--since`, `--snapshot`. |
| `ask <question>...` | One-shot Chat question; `--machine`, `--project`, `--json`. |
| `mcp` | Serve read-only MCP tools over stdio; `--snapshot` forces one-shot collection for read tools without history. |
| `doctor` | Setup/collection diagnostics; `--json`. |
| `init` | Optional setup, previewing changes; `--yes/-y` accepts each prompt. |
| `config path` / `config show` / `config edit` | Configuration path, values, or editor; only `path` supports `--json`. |
| `collect` | Collector; `--once`, `--serve`, `--listen`, `--cpu-profile`, `--profile-for`. Explicit `--serve=false` overrides configuration. `--once` cannot serve; `--listen` requires serving. |
| `service install` / `start` / `stop` / `restart` / `status` | Manage the collector service. |
| `service logs [-f]` | Show logs; `--follow/-f` is scoped to this command. |
| `service uninstall` | Remove the collector service. |
| `hooks install [<adapter>]` / `hooks uninstall [<adapter>]` | Adapter defaults to `all`; positional `claude\|codex\|copilot\|all`. `--dry-run`, `--yes/-y`, `--otlp`, `--traces` (requires OTLP). |
| `hooks status [<adapter>]` | Adapter defaults to `all`; `--otlp` selects telemetry status. |
| `fleet peers list` | Trusted data-source machines; `--json`. |
| `fleet peers add <host[:port]>` | Pair with a source; `--name`, `--device-id`, `--json`. Headless pairing requires its device ID; `--yes/-y` also requires that ID. |
| `fleet peers remove <peer>` | Confirm local peer removal; `--purge` deletes cached rows; `--yes/-y`. |
| `fleet viewers list` | Viewer access grants; `--json`. |
| `fleet viewers revoke <viewer>` | Confirm viewer revocation; `--yes/-y`. |
| `fleet enroll` | Open pairing window; `--allow-viewer`, `--allow-ack`, `--window`. Headless enrollment requires the viewer device ID; `--yes/-y` also requires that ID. |
| `fleet rekey` | Confirm identity regeneration, invalidating all pairings; `--yes/-y`. |
| `fleet id` / `fleet status` | Device ID or collector serving state; `--json`. |
| `version` / `--version` | Build metadata or version discovery; `version` supports `--json`. |

Read-view options are `--machine`, `--project`, `--since`, `--snapshot`,
`--watch=<duration>`, `--json`, and `--no-color`, on the commands listed above.
Unsupported flags and conflicting selectors are errors. `--theme` is scoped to
`top`. Flags belong after the command or its explicit `list` verb.

All prompts support `--yes/-y`. When stdin is not a terminal, a command that
needs confirmation exits 2 with **not applied; pass --yes to apply without
asking**. `--force` only escalates termination; it does not authorize it.
Pairing requires its identity arguments rather than blanket consent. Hidden
agent hooks remain quiet and fail open with exit 0.

Exit codes: **0** success; **1** failure, refusal, or partial completion;
**2** invalid usage or missing required non-interactive input. Bulk cleanup
returns 1 if any item fails, with completed/skipped/failed counts in text or JSON.

Hidden integration interfaces are unchanged: `hook <adapter> <event>`, `feed`,
`dev`, `mcp --chat`, and the collector's scheduled-service flags. MCP tool names
and fields, configuration keys (including `inbox`), `DRIGA_*` environment names,
JSON field names, web routes, Chat action tool names/records and `[[session:N]]`
citations retain their contracts. MCP `get_agent` maps to `agents show`,
`inspect_project` to `projects show`, `inspect_setup` to `doctor`, and
`list_cleanup_candidates` to `cleanup list`.

## Command migration

There are no aliases, deprecation shims, or legacy command spellings.

| Old | New |
| --- | --- |
| `kill <id>` | `orphans kill <orphan-id>` |
| `agent <id>` | `agents show <session-id>` |
| `where <project>` | `projects show <project>` |
| `summarize <project>...` | `projects summarize <project>...` |
| `summarize` (implicit all) | `projects summarize --all` |
| `jump <item-id>` | `inbox jump <item-id>` |
| `cleanup <id> [--yes]` | `cleanup remove <candidate-id> [--yes]` |
| `cleanup --all --kind <kind>` | `cleanup remove --all --kind worktree\|scratch` |
| `cleanup` (listing) | `cleanup` or `cleanup list` |
| `machine [show]` | `machines show [<machine=local>]` |
| `machine show --machine <machine>` | `machines show <machine>` |
| `machine reset-id` | `machines reset-id` |
| `serve [--listen <address>]` | `collect --serve [--listen <address>]` |
| `service <action>` | `service install\|start\|stop\|restart\|status\|logs\|uninstall` (real subcommands; only logs accepts `-f`) |
| `fleet peers` | `fleet peers list` |
| `fleet add <host[:port]>` | `fleet peers add <host[:port]>` |
| `fleet remove <peer>` | `fleet peers remove <peer>` |
| `fleet viewers` | `fleet viewers list` |
| `fleet revoke <viewer>` | `fleet viewers revoke <viewer>` |
| `fleet enroll --allow <id>` | `fleet enroll --allow-viewer <id>` |
| `fleet enroll --ack` | `fleet enroll --allow-ack` |
| `fleet add --id <id>` | `fleet peers add --device-id <id>` |
| `hooks <verb> --agent <adapter>` | `hooks <verb> <adapter>` |
| Global view flags on unrelated commands | Command-scoped view flags; unsupported combinations are rejected |
| No CLI session jump/kill | `agents jump <session-id>` / `agents kill <session-id>` |
| `version` only | `version` and `--version` |

Machine reset, fleet rekey, peer removal and viewer revocation now require
confirmation or `--yes/-y`. Restart collectors after upgrading. Previously
issued desktop notifications still contain the removed `jump` spelling; dismiss
those notifications. Newly issued notifications use `driga inbox jump`.
