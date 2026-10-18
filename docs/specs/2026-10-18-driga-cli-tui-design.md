# Driga — CLI/TUI v1 design

Status: approved 2026-10-18 · Date: 2026-10-18 · Name: **Driga** (from Sanskrit *dṛg*, "eye, sight"); domains driga.run / driga.dev unregistered as of 2026-10-18

## 1. Intent

Developers increasingly run several AI coding agents in parallel — interactive CLIs (Claude Code, Codex, Gemini CLI, …), headless runs (`codex exec`, `claude -p`), agents embedded in desktop apps or editor plugins — across many repos and worktrees, often with one agent supervising others. They lose track of:

- which agent is doing what, where, and whether it is working, waiting for them, stuck, or dead;
- where each project stands (last thing done, next step, uncommitted work at risk);
- what the machine itself is doing: helper processes left behind by finished sessions pile up unnoticed. (Motivating incident on the author's Mac: nine orphaned agent-plugin daemons held ~3.8 GB across 352 processes and swap filled.)

Driga is one terminal tool that answers "who is doing what, where, and what needs me?" in under a second, for humans (TUI/CLI) and for agents (`--json` now, MCP in v1.5).

It will be open-sourced later, so v1 makes OSS-friendly choices now (config over hard-coded paths, per-agent adapters, privacy-safe defaults) without paying the full OSS polish cost yet.

## 2. Goals, non-goals, anti-goals

**Goals (v1)**
1. Agent view: every live agent session on the machine with type, project, worktree/branch, title/last prompt, state, age, memory.
2. Project view: every repo with live agents, dirty/unpushed state, worktree count, last activity, and a "where it stands" line.
3. Orphan detection: process trees left behind by dead agent sessions, with memory totals and the exact command to remove them.
4. Machine health: memory pressure, swap, load, disk, plus memory used by agent processes grouped by type.
5. Attention inbox: sessions waiting for the user, stuck, or finished and not yet looked at.
6. File-conflict warnings: two live agents editing the same file or sharing one worktree.
7. Fast: any CLI command < 200 ms; TUI refresh every 1 s without visible lag.
8. Lightweight: background collector RSS < 50 MB, < 1% avg CPU.

**Non-goals (v1)** — planned later, design must not block them
- Web UI / PWA / push notifications (v2).
- MCP server (v1.5) — v1 provides `--json` so agents can already shell out.
- LLM summaries / `ask` chat via `claude -p` (v1.5).
- Multiple machines / fleet hub (v1.5–v2). v1 is single-machine but every row carries `machine_id`.
- Dispatching tasks to machines (v2).
- Gemini adapter (v1.5).
- Linux support (before OSS release).

**Anti-goals** — would make it a failure even if it "works"
- Driga itself adding meaningful load: no fs watchers over repos, no per-session child processes, no per-agent MCP stdio servers.
- Storing full transcripts, prompts beyond a short truncated snippet, or secrets. Metadata only.
- Taking destructive actions without explicit confirmation. Nothing is killed, pruned, or edited automatically.
- Writing to agent config files (`~/.claude/settings.json`, `~/.codex/config.toml`) except via an explicit, reversible `driga hooks install` that backs up first.
- Being wrong confidently: unknown states display as `unknown`, never guessed as `working`.

## 2a. Generality — not fitted to one setup

The author's Mac motivated Driga, but nothing may be tuned to it.
- **No user-specific defaults**: no default project roots, terminal, shell, editor, or agent mix. Everything is discovered (agent session dirs, session cwds, `$TERM_PROGRAM`) or configured.
- **Rules from mechanisms, not names**: liveness from pid/registry evidence, orphans from process ancestry and reparenting, activity from log growth. Name lists (helper signatures, agent binaries) live in a data file (`signatures.toml`) that ships with the binary and that users can extend; no detection depends on one person's tools.
- **Adapters are version-gated**: each adapter declares the agent versions whose formats it was tested against; unknown versions run in tolerant mode and say so in `driga doctor` instead of guessing.
- **Validation on held-out data**: thresholds (`stuck_after`, orphan age, activity windows) and state rules are tuned on one set of real histories and validated on a different set — sessions from other machines and other people (volunteers' scrubbed fixtures), and synthetic traces covering agents and versions the author doesn't use. A rule that only works on the tuning set doesn't ship.
- **Fixture corpus**: `testdata/fixtures/<adapter>/<version>/` from at least three machines before release; generator for synthetic process trees and session logs (crashes, partial lines, rotations, long-idle sessions, many-worktree repos, one-repo users).
- **Configurable, documented defaults**: every threshold is in config with its rationale in docs.
- **Small-setup first-class**: one agent + one repo + no daemon must be a good experience (journey 6), not a degenerate case of a 59-repo setup.

## 3. Constraints

- macOS (Apple Silicon) first; Darwin-specific code isolated behind a platform interface.
- Single static Go binary, no cgo (cross-compile later).
- No network access required; no Anthropic API key; no telemetry.
- Agent log formats are undocumented and drift between versions → tolerant parsers, fixture tests.
- Go chosen for single-binary distribution, cross-compilation, and low memory; no dependency on any particular user's toolchain.

## 4. Decided by the user

- Build custom (no existing tool covers project-level state across agent types and machines); reuse existing tools for *running* sessions (tmux, Agent View, etc.).
- Open-source later; others must be able to use it.
- **CLI/TUI first**; web later.
- No Anthropic API key; LLM features go through the user's own CLI (`claude -p`) later.
- Go stack (accepted the OSS-oriented recommendation; stack list in §11).

## 5. Architecture

```
            ┌──────────── driga (single binary) ────────────┐
 launchd ──►│ driga collect  (daemon)                        │
            │   ├─ sources: procs · machine · git · claude · codex · hooks
            │   ├─ derive: states · orphans · conflicts · inbox
            │   └─ write → SQLite (WAL)  ~/Library/Application Support/driga/driga.db
            │                                                │
 terminal ─►│ driga <cmd> / driga top  (reader)              │
            │   └─ read SQLite (read-only conn) → table / JSON / TUI
            └────────────────────────────────────────────────┘
 agent hooks ─► driga hook <event>  (tiny, appends JSONL to ~/.driga/events/)
```

- **One writer** (the collector daemon), **many readers** (CLI/TUI). SQLite WAL makes this safe and fast.
- The daemon is **recommended but opt-in** (`driga service install`, a per-user LaunchAgent). Without it, every command works in **snapshot mode**: a one-shot collect (~1–2 s) into a temporary in-memory view, clearly labelled `snapshot · no history` in the header and in JSON. Snapshot mode never writes the persistent DB, so it can't create false history. The "install the service" hint appears once per day at most and can be silenced in config.
- **Collector exclusivity**: one collector per machine, enforced by an `flock` on `driga.lock` in the data dir; a second `driga collect` exits with a clear message.
- **Adaptive cadence**: when no viewer is open, the daemon slows down (procs 60 s, git 5 min, machine 60 s); while a TUI or `--watch` is running, it speeds up (procs 5 s, git 30 s, machine 10 s). Viewers signal presence by touching `viewer.heartbeat` in the data dir.
- **Freshness and coverage are first-class**: every source records `last_ok_at` and its last error; every view and JSON payload carries `observed_at`, per-source freshness, and any coverage gaps (e.g. "daemon was down 02:10–07:45"). Stale data is shown dimmed and labelled, never as current.
- The <50 MB RSS budget is a target, not a promise: it is measured as peak memory including subprocesses (git, lsof), plus wakeups per minute, on large real histories from several machines (author + volunteers) before v1 ships.
- Hooks never touch the DB: they append one JSON line to a spool file and exit (< 20 ms, never block the agent). The daemon tails the spool.
- Every table row has `machine_id` so a future hub can merge machines without schema change.

## 6. Data sources and adapters

Each agent type is an **adapter** implementing:

```go
type Adapter interface {
    Name() string                                   // "claude", "codex", ...
    Discover(ctx) ([]SessionRef, error)             // find live + recent sessions
    Tail(ctx, ref, offset) (events []Event, newOffset int64, err error)
    ClassifyProcess(p Proc) (role ProcRole, ok bool) // is this pid ours? interactive / headless / helper / mcp
    InstallHooks(dryRun bool) (Diff, error)         // reversible
    UninstallHooks(dryRun bool) (Diff, error)
}
```

### 6.1 Claude Code (formats observed in Claude Code 2.1.x; version-gated)
- **Live registry** `~/.claude/sessions/<pid>.json`: `sessionId, cwd, name, kind, entrypoint, version, startedAt, status (idle|busy|shell), statusUpdatedAt`. Primary liveness/state source. Stale files (pid gone) ⇒ session dead.
- **Transcripts** `~/.claude/projects/<slug>/<sessionId>.jsonl`: per entry `cwd, gitBranch, timestamp`; `last-prompt`, `ai-title`; assistant `tool_use` (Edit/Write/MultiEdit/NotebookEdit inputs ⇒ files touched); `system` entries with `stopReason`. Tail incrementally by byte offset.
- Sessions whose cwd is inside the OS temp dir (`$TMPDIR`, `/tmp`, `/private/tmp`) are grouped as "temporary" instead of being attributed to a project.
- **Hooks** (via `driga hooks install`): `Stop` (finished turn), `Notification` (waiting for permission/input), `SessionStart`, `SessionEnd`. Each runs `driga hook claude <event>`; payload from stdin is reduced to `{ts, session_id, cwd, event, message≤200 chars}`.

### 6.2 Codex (formats observed in Codex CLI 0.16x; version-gated)
- **Rollouts** `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`: `session_meta` (cwd, originator, source, cli_version, git{branch, commit, repo}), `turn_context` (model, approval_policy, sandbox), `event_msg` (task_started, item_completed, token_count incl. rate_limits), function/custom tool calls (apply_patch ⇒ files touched).
- **Catalog** `~/.codex/codex-dev.db` → `local_thread_catalog(display_title, cwd, git_branch, source_kind, timestamps)`, opened read-only (`mode=ro&immutable=0`), schema checked at startup; adapter degrades gracefully if absent.
- **Processes**: `codex` TUI, `codex exec` (headless), `codex app-server`, plus Codex hosted inside other apps or plugins (e.g. a desktop app, an editor or Claude Code plugin broker). Process roles are matched from the signature data file (§7.2), not hard-coded.
- **Hook**: `notify` in `~/.codex/config.toml` is a single command and is often already used by another tool (on the author's Mac, by Codex Computer Use). Rule: if `notify` is unset, `driga hooks install` may set it (opt-in, reversible); if it is set, Driga leaves it alone and relies on rollout tailing. A chaining wrapper can be considered post-v1.
- **Hosted threads** (desktop apps, `codex app-server`, editor/agent plugins): in scope. Many threads share one long-lived host process, so liveness is not taken from the process; state is mtime-based and always rendered as heuristic (`~`): rollout grew < 60 s ⇒ `~working`, last turn completed ⇒ `~idle`, no growth > 24 h ⇒ hidden from live views. Thread identity and title come from rollouts plus `local_thread_catalog`; `originator`/`source` distinguishes TUI, exec, desktop app, and plugin.

### 6.3 Processes (gopsutil)
Every 15 s: one full process snapshot (pid, ppid, rss, cpu, start time, cmdline). `cwd` via `lsof -a -d cwd -Fn -p <pids>` batched, only for agent-classified pids, every 60 s.

### 6.4 Machine
Every 30 s: memory (used, compressed, wired), swap used/total, `memory_pressure` free %, load 1/5/15, cores, disk free on `/`. Kept 24 h at 30 s resolution, then downsampled to 5 min for 7 days.

### 6.5 Git
- Repos come from (1) the cwds of every discovered agent session, resolved to their git root, and (2) optional config roots (no default root; `driga init` suggests common code directories it finds). So a user with one repo and no config still sees their project.
- Per repo: `git worktree list --porcelain`; per worktree, a fast pass skips when `HEAD`/`index`/`logs/HEAD` mtimes are unchanged, but a **full `git status` runs at least every 5 min** (adaptive cadence) regardless, because ordinary working-file edits don't touch those files. `git status --porcelain=v2 --branch --untracked-files=no` (+ untracked count every 5th pass), last commit time/subject. Concurrency 4. Measured: ~1 s total for all repos.
- Ahead/behind from `--branch` output (no fetch; Driga never touches the network).

## 7. Derived state

### 7.1 Agent states
State is four separate facts, not one guess:
- **liveness**: `alive · dead · unknown` (process / registry evidence)
- **activity**: `working · idle · quiet` (log growth, registry status)
- **completion**: a turn-finished *event* with id and timestamp (hook `Stop`, Codex turn completion)
- **attention**: an open *request* (hook `Notification`) or none

The single displayed label is derived from those: `working · waiting · idle · stalled? · finished · dead · unknown`. **`stalled?`** replaces "stuck": silence plus low CPU is only a suspicion, always shown with the question mark and the evidence ("busy, no log growth 14 m, CPU 0.2%"). Every label in detail views and JSON carries its evidence and `observed_at`.

| Adapter | Rule (first match wins) |
|---|---|
| Claude | hook `Notification` newer than last activity ⇒ **waiting**; registry `busy` + transcript mtime < `stuck_after` ⇒ **working**; registry `busy` + no transcript growth for `stuck_after` + CPU < 1% ⇒ **stalled?**; hook `Stop` newer than last user prompt and not yet seen ⇒ **finished**; registry `idle`/`shell` ⇒ **idle**; registry file but pid gone ⇒ **dead** |
| Codex TUI/exec | rollout mtime < 60 s + process alive ⇒ **working**; turn completed ⇒ **idle** (+ completion event ⇒ **finished**); process gone after completion ⇒ **finished**, without ⇒ **dead**; no growth > `stuck_after` mid-turn ⇒ **stalled?**. Codex approval-waiting is **not** inferred (a tool call without output doesn't prove a pending approval); it shows as `quiet` until Codex exposes a reliable signal |
| Unmatched agent-looking process | **unknown** |

`stuck_after` default 10 min (config). Heuristic states render with a `~` marker.

### 7.1b Inbox semantics
- Inbox items are **events**, not sessions: each completion event and each attention request is one item with a stable id (`adapter:session:event-offset`), deduplicated on replay after daemon restarts.
- **Inspecting never acknowledges.** Opening a detail pane, `driga agent <id>`, and every `--json` query are pure reads. Acknowledgment is explicit: `space`/`x` in the TUI or `driga inbox ack <item-id>|--all`.
- Acks are written as spool events (like hooks), so readers stay read-only and the daemon remains the only DB writer. The ack command waits for the daemon's receipt (≤ 2 s) and reports if the daemon isn't running.
- **First-import baseline**: on first run, everything that happened before install is marked as already seen, so old sessions don't flood the inbox.
- Attention requests from a family's workers surface on the parent's inbox entry (a blocked worker is never hidden by grouping).

### 7.1a Session families (supervisor → workers)
Sessions may have a **parent**: a session started by another agent (a `codex exec` or Codex-plugin call made from a Claude session, a `claude -p` launched by a script running under an agent, a Claude subagent).
- Link by process ancestry first (child pid's ancestor chain contains the parent agent's pid), then by plugin/broker metadata where available (broker cwd + start time inside the parent's lifetime).
- `sessions.parent_session_id` (nullable) stores the link.
- A headless child that exits normally becomes **finished** (not dead). Only **top-level** sessions create inbox items; a finished child shows as a count on its parent ("3 workers finished").
- Same-worktree conflicts are **suppressed inside one family** (supervisor + its workers sharing a worktree is expected); same-file conflicts within a family are still shown but marked `family`.
- Agents tab groups children under their parent (collapsed by default, `→` expands).
- An orphan's "spawning session dead" check uses this parent link; PPID 1 alone is not enough.

### 7.2 Orphans
A process tree is an **orphan candidate** when its root is reparented to launchd (PPID 1) or its recorded spawning session (§7.1a) is dead, AND it is an agent helper — either its process ancestry shows it was spawned by a known agent session, or it matches the shipped signature data file (`signatures.toml`: agent app-servers and plugin brokers, `<agent> mcp serve`, generic MCP-server launch patterns such as `npx … mcp`/`uvx … mcp`; users and contributors extend it without code changes), AND it shows no client activity (no CPU and no open client sockets beyond its own children over the observation window). A matching `cwd` with a live session is shown as evidence ("live session in same dir") but is **not** proof of ownership either way.
- Candidates younger than `orphan_min_age` (default 10 min) are listed as `young` (visible, not offered for kill), so fresh leaks aren't hidden.
- Each candidate has a stable id (`root_pid:start_time`) and ownership evidence. Memory is shown as "~estimated reclaim" (RSS over-counts shared pages).
- Killing targets **one candidate**: `driga orphans kill <id>` or `k` in the TUI. Immediately before signaling, Driga re-validates the root's pid + start time and re-walks its descendants; if anything changed it aborts. `driga orphans kill --all` asks y/N per candidate.
- Processes belonging to a running app bundle (their `.app`'s main process is alive) are never orphans.

### 7.3 Conflicts
**Current** conflicts use edits by live sessions in the last 30 min; anything older is shown separately as "recent overlap (last 6 h)", never as a current collision. Report:
- **same-worktree**: two live sessions with the same cwd/worktree (common, cheap);
- **same-file**: same absolute path edited by two live sessions;
- **same-file-other-worktree**: same repo-relative path on the same branch in different worktrees.

### 7.4 "Where it stands" (no LLM in v1)
Per project, a composed line from, in order: latest `Stop`-hook handoff (only if text capture is enabled, §13) → Claude `ai-title` / Codex `display_title` of the most recent session → last commit subject. Each part shows its source and age, so a stale title never reads as a fresh handoff. v1.5 replaces this with a cached `claude -p` summary. (README/CLAUDE.md "goal" extraction is cut from v1.)

### 7.5 Project activity
`active` (agent live or activity < 24 h) · `recent` (< 7 d) · `idle` (older). **At-risk** flags: uncommitted changes whose oldest dirty file is older than 7 d (age = oldest mtime among dirty files); unpushed commits older than 3 d; > 10 worktrees.

## 8. CLI

Binary `driga`. Global flags: `--json`, `--project <path|name|.>` (scope every view to one project), `--since <dur|yesterday>`, `--snapshot` (force snapshot mode), `--machine <id>` (future), `--no-color`, `--watch[=2s]`.

```
driga                      one-screen summary (agents by state, inbox count, swap/load, orphans, collection mode)
driga init                 first-run: detect agents/sources, suggest roots, offer service + hooks (each optional)
driga top                  TUI
driga agents [--all]       live agents (--all includes dead/finished last 24h)
driga agent <id>           detail: last prompt (if captured), recent tool calls, files touched, procs, evidence
driga projects [--at-risk|--active]
driga where <project>      where it stands, worktrees, agents, last handoffs
driga inbox [--since yesterday]
driga inbox ack <id>|--all
driga conflicts
driga orphans              candidates with ids, evidence, ~reclaim
driga orphans kill <id>|--all   revalidate, SIGTERM, report
driga machine              health + top agent memory users
driga doctor               setup checks (collection mode, source freshness, hooks, permissions)
driga hooks install|uninstall|status [--dry-run]
driga service install|uninstall|start|stop|restart|status|logs
driga collect [--once]     run collector (what launchd runs)
driga config path|show|edit
driga version
```

JSON output is stable and versioned (`{"schema":1,...}`); every payload includes `observed_at`, `mode` (`daemon|snapshot`), per-source `freshness`, and `coverage_gaps`, so an agent never mistakes stale data for current. MCP later reuses the same shapes.

### 8.1 How people use it (journeys)
1. **First 5 minutes**: `brew install …` → `driga` shows discovered sessions immediately in snapshot mode, plus one line "snapshot · no history — `driga service install` for inbox & history". `driga init` optionally sets roots, service, hooks; each step is skippable and explains what it changes.
2. **Morning check-in**: `driga` → `driga inbox --since yesterday` (overnight completions and open requests) → `driga where api`.
3. **Mid-session "what needs me"**: `driga top` → `3` Inbox → arrows/enter for evidence → `x` ack → `o`/`a` to jump to the session.
4. **Supervisor agent**: `driga inbox --json`, `driga agents --json --project .`, `driga agent <id> --json`; reads never ack; freshness fields tell it when data is stale.
5. **Orphan cleanup**: `driga orphans` → `driga orphans kill <id>` (y) → `driga machine` to confirm memory dropped.
6. **One agent, one repo**: `driga top --project .` hides empty tabs and global scanning; works without the service.
7. **Later, two Macs**: `driga machines`, `driga --machine mini inbox`; offline machines stay visible as stale (fleet design is out of v1 scope).

## 9. TUI (`driga top`)

```
driga · laptop · swap 15.1/16G ▇▇▇▇▇▇▇░ · load 19/14 · 7 agents (3 working, 1 waiting, 1 stalled?) · 2 orphans 3.0G
 [1 Agents]  2 Projects   3 Inbox(2)   4 Orphans(2)   5 Machine           ? help  q quit
──────────────────────────────────────────────────────────────────────────────────────
 STATE      AGENT   PROJECT          BRANCH            TITLE / LAST PROMPT          AGE   MEM
 ● working  claude  api              main              add rate limiting             2h   196M
 ◐ waiting  claude  web              feat/onboarding   "approve edit to form.tsx?"   40m  190M
 ? stalled? codex   billing          fix-invoices      invoice rounding bug          31m   84M
 ○ idle     gemini  docs             main              docs pass                     1d   120M
 ...
──────────────────────────────────────────────────────────────────────────────────────
 enter detail · / filter · s sort · g group by project · o open in terminal · a attach tmux · k kill(orphans)
```

- Tabs 1–5; `enter` opens a detail pane (viewport) for the selected row; `esc` back.
- Header strip always visible: machine health + counts + orphan memory.
- Projects tab: one row per repo (worktrees collapsed, `→` expands), columns activity, agents, dirty, unpushed, worktrees, where-it-stands.
- Actions always confirm (`y/N`), and print the equivalent CLI command.
- `o` opens a new terminal in the session's cwd (`open_cmd` config; default derived from `$TERM_PROGRAM` for common terminals, falling back to the OS default terminal); `a` runs `tmux attach -t …` only if the pid is inside tmux (else the key is hidden). For Claude sessions the detail pane also shows the exact resume command (`claude --resume <id>` in that cwd).
- `space`/`x` acknowledges the selected inbox item; viewing never acknowledges.
- With `--project`, tabs with nothing to show are hidden.
- Refresh: poll DB every 1 s; DB `PRAGMA data_version` check avoids redundant re-renders.
- Colors adapt to light/dark terminal; `--no-color` and `NO_COLOR` honored.

## 10. Storage

SQLite (WAL, `synchronous=NORMAL`), migrations embedded (goose). Main tables:

```
machines(id, hostname, os, arch, cores, mem_total, first_seen, last_seen)
repos(id, machine_id, path, name, remote_url, root_group)
worktrees(id, repo_id, path, branch, head, ahead, behind, dirty, untracked, last_commit_at, last_commit_subject, updated_at)
sessions(id, machine_id, adapter, external_id, parent_session_id, origin, pid, cwd, repo_id, worktree_id, kind, title, last_prompt, state, state_heuristic, started_at, last_activity_at, ended_at, seen_at, source_path)
session_files(session_id, path, repo_rel_path, last_edit_at)          -- for conflicts
events(id, machine_id, session_id, ts, kind, summary)                  -- capped per session (last 200)
handoffs(session_id, ts, text)
procs(machine_id, pid, ppid, start_time, rss, cpu, cmd, role, session_id, cwd, seen_at)  -- latest snapshot only
orphans(machine_id, root_pid, start_time, signature, proc_count, rss_total, cwd, first_seen, last_seen)
machine_samples(machine_id, ts, mem_used, mem_compressed, swap_used, swap_total, pressure_free, load1, load5, load15, disk_free)
offsets(machine_id, path, inode, size, offset, mtime)
```

Also: `inbox_items(id, session_id, kind, ts, summary, acked_at)` and `sources(machine_id, name, last_ok_at, last_error, last_error_at)` for freshness, plus `gaps(machine_id, from_ts, to_ts)` recorded when the daemon starts after downtime.

Retention: events 30 d, machine_samples per §6.4, dead sessions 30 d. (No scheduled VACUUM in v1.)

## 11. Stack (versions checked 2026-10-18)

Go 1.27 · CLI `alecthomas/kong` v1.16.1 · TUI `charm.land/bubbletea/v2` v2.1.0 + `lipgloss/v2` v2.0.6 + `bubbles/v2` v2.2.1 · SQLite `modernc.org/sqlite` v1.60.1 (pure Go) · migrations `pressly/goose/v3` v3.29.0 · queries `sqlc` v1.31.1 (codegen only) · processes/machine `shirou/gopsutil/v4` v4.26.9 · service `kardianos/service` v1.3.0 · config `pelletier/go-toml/v2` v2.4.3 · logging `log/slog` · tests stdlib `testing` + `google/go-cmp` · lint `golangci-lint` · release `goreleaser` (later).

Git via `git` CLI (`os/exec`), not go-git. No fsnotify.

## 12. Config

`~/.config/driga/config.toml` (XDG; created with commented defaults on first run):

```toml
# roots = ["~/code"]           # optional extra repo discovery; sessions' cwds are always used
root_depth = 3
exclude = ["**/node_modules", "**/.tmp/**"]
stuck_after = "10m"
orphan_min_age = "10m"
capture_text = "titles"          # "titles" | "snippets"
# no_service_hint = false
# open_cmd = "open -a Terminal {cwd}"   # default: auto-detected terminal
[adapters.claude]  enabled = true
[adapters.codex]   enabled = true
[orphans] extra_signatures = []
```

## 13. Privacy & safety

- **Text capture is opt-in.** Default `capture_text = "titles"`: only model-generated titles (Claude `ai-title`, Codex `display_title`) and file paths are stored. `capture_text = "snippets"` (set by the user, e.g. via `driga init`) also stores last-prompt and handoff snippets. Scrubbing reduces risk but cannot guarantee privacy, so it is not relied on as the gate.
- Captured snippets are truncated (≤ 200 chars) and passed through a secret scrubber (API-key/token regexes: `sk-…`, `ghp_…`, `xox…`, AWS keys, JWTs, `-----BEGIN … KEY-----`) before write.
- DB and spool files `0600`, dirs `0700`.
- Read-only access to agent data; Codex sqlite opened read-only.
- Kill: SIGTERM to tree leaves→root, wait 5 s, report survivors; SIGKILL only with `--force` and a second confirmation.
- `hooks install`: backs up target file to `*.driga-bak-<ts>`, merges (never replaces) hook arrays, prints diff, requires `y`. `uninstall` removes only entries tagged as Driga's.

## 14. Repo layout

```
driga/
  cmd/driga/main.go
  internal/
    cli/            kong commands
    tui/            bubbletea models (app, tabs, detail, header)
    collector/      scheduler, loops, one-shot mode
    adapter/        adapter.go, claude/, codex/
    platform/       darwin.go (pmset/memory_pressure/lsof), proc.go (gopsutil)
    gitscan/
    derive/         states, orphans, conflicts, inbox, projects
    store/          sqlite open, migrations/, queries (sqlc)
    hooks/          spool writer + installers
    scrub/
    config/
  testdata/fixtures/{claude,codex}/   scrubbed real samples
  docs/
```

## 15. Testing

- Adapters: golden tests on scrubbed fixture JSONL/registry files (incl. partial trailing lines, truncation, inode change).
- Derive: table tests for state rules, orphan rules (fake process trees), conflicts.
- Store: migrations up on empty DB; query tests against temp DB.
- Collector: one-shot run against a temp HOME with fixtures; assert DB contents.
- TUI: model update tests (key → state) + `teatest` snapshot of the Agents tab at fixed width.
- Manual acceptance on several machines (see §2a), comparing `driga agents` against `ps` and each agent's own session list, and `driga orphans` against hand-labelled process trees.
- Self-footprint check: `driga doctor` reports collector RSS; CI-free check script asserts < 50 MB after 10 min.

## 16. Delivery phases

1. **Skeleton**: repo, kong CLI, config, store + migrations, `version`, `doctor` (stub).
2. **Machine + procs**: platform layer, process snapshot, machine samples, `driga machine`.
3. **Git**: repo discovery, worktree scan, `driga projects`, `where` (git part).
4. **Claude adapter**: registry + transcript tailing, sessions, states (no hooks), `driga agents`, `agent <id>`.
5. **Codex adapter**: rollouts + catalog + process roles.
6. **Derive**: orphans (+ `--kill`), conflicts, inbox, project activity/at-risk, where-it-stands.
7. **Hooks**: spool writer, `driga hook`, installers (claude, codex) with dry-run/backup, waiting/finished states.
8. **Collector daemon**: scheduler, launchd via `service install`, one-shot fallback, retention.
9. **TUI**: header, 5 tabs, detail pane, actions with confirm.
10. **Hardening**: scrubber, fixtures, footprint check, README.

Phases 2–5 are parallelizable after phase 1.

## 17. Left to the builder

Exact TUI colors/glyphs; table column widths; internal package boundaries beyond §14; sqlc vs hand-written queries for small tables; log rotation details; exact orphan signature entries (seed `signatures.toml` from each supported agent's documented helper processes, then validate per §2a).

## 18. Calls made by Claude (not confirmed by the user — check these)

- Go stack (user accepted Go-for-OSS direction; templ/web parts dropped with web).
- Always-on collector daemon via launchd, opt-in, with labelled snapshot mode when not installed (user: "whatever is best"; Codex review concurred).
- `stalled?` (suspicion) = busy + no progress 10 min + CPU < 1%; orphan `young` threshold 10 min.
- Text capture defaults to titles only; prompts/handoff snippets opt-in.
- Repos discovered from session cwds; no default root.
- Repo = unit, worktrees nested.
- Hooks installed only via explicit `driga hooks install`; Codex `notify` is only set when unset, never replaced.
- Session families: parent link via process ancestry; only top-level sessions hit the inbox; same-worktree conflicts suppressed within a family.
- Codex threads hosted by the desktop app / app-server / plugin broker are shown with heuristic (`~`) mtime-based states.
- Inbox acks go through the spool, so the daemon is the only DB writer.
- Gemini deferred to v1.5.
- Name: Driga (user decision); binary `driga`.
- Config at `~/.config/driga/config.toml`, data in `~/Library/Application Support/driga/`.
