# Machine health — design

Date: 2026-10-20. Status: approved in chat by the maintainer ("Sure"); build order: parts 1 and 4 first, then 3, then 2 after Chat phase 2. Items marked *(author's call)* were not decided by the maintainer.

## Why

On a heavily loaded maintainer Mac (swap 81 of 83 GB, load 11–17 on 14 cores, 25 GB free disk) driga could explain only the agents' share (about 6.5 of 34 GB in use). The rest came from Chrome, Backblaze, a VM and the terminal, found by hand with `top`. Dev servers started by agent sessions (two `workerd` processes at about 70% CPU under the ekam project) were not tied to their sessions. Disk cleanup covered agent worktrees and scratch only, while the large safe wins were tool caches, dependency folders and old agent CLI versions. Nothing warned before it got this bad.

## Goals

1. **Whole-machine view.** Top apps by memory and CPU, with each process grouped into its app and, where provable, attributed to the agent session that started it.
2. **Pressure advice.** When the OS reports pressure, list what would relieve it, largest first, each with its reclaim estimate; actions come with Chat phase 2.
3. **Wider disk cleanup.** Tool caches, dependency folders in idle projects, and old agent CLI versions, with evidence and a "rebuilds itself" or "your data" label.
4. **Early warnings.** Desktop alerts for low disk and critical memory pressure through the existing alert path.

## Non-goals

- Killing or pausing non-agent apps (Chrome, Backblaze) from driga; driga names them, the user closes them.
- Deleting agent history (`~/.claude/projects`, `~/.codex/sessions`); it stays report-only with pointers to each agent's own retention setting. Codex `archived_sessions` is listed as "your data" and only removable by an explicit per-item choice.
- Windows.

## Constraints

- Not curve-fitted (design §2a): triggers come from OS-reported signals (macOS memory-pressure level from `memory_pressure`/`kern.memorystatus_vm_pressure_level`, Linux PSI already read in `procfs.go`, swap used versus total), and user-set limits with documented defaults. No threshold is tuned to the maintainer's machine.
- Read-only by default. Cleanup runs only on an explicit confirm, as today, and prefers each tool's own command over deleting files.
- Per-pass cost stays within the collector's existing budgets (heap-budget tests must still pass): the whole-machine view reuses the process sample driga already takes; no new full process scans per pass.

## Part 1: whole-machine view

- **Memory measure.** On macOS, per-process physical footprint (includes compressed memory; matches Activity Monitor) where readable without elevated rights; fall back to RSS and label which was used. Linux: RSS plus swap from `/proc/<pid>/status` (`VmSwap`).
- **App grouping.** Walk each process's parent chain to the nearest `.app` bundle executable (the same rule the alert-click jump uses); processes outside bundles group by executable name. Helpers count toward their app.
- **Agent attribution.** A process is attributed to a session only with proof: its parent chain reaches the session's process, or its working directory is inside a live session's project and it started after that session. Shown as "← ekam · session #251". Otherwise unattributed.
- **CPU.** Per-process CPU over the last sample interval from the existing deltas; app totals.
- **Surfaces.** A "Machine" section in the TUI Machine tab and the web Machine page: totals (used, compressed, swap, pressure level), top 10 apps by memory and by CPU, each expandable to its processes and attribution. MCP `get_machine` returns the same data so Chat can answer "why is my Mac slow?".

## Part 2: pressure advice (after Chat phase 2)

When pressure is warning or critical, or swap use is above the user limit: list idle or finished agent sessions with their (and their attributed helpers') footprint, and running agent CLI processes whose executable is not the newest installed version of that agent. Each line has a reclaim estimate; the action ("Close session #403 · 1.2 GB") is a Chat action card under the trust mode.

## Part 3: wider disk cleanup

New `driga cleanup` kinds alongside worktree, scratch and history:

| Kind | Candidates | How it is cleaned | Label |
|---|---|---|---|
| `cache` | Go build cache, Go module cache, npm cache, unavailable iOS simulators, Homebrew downloads | the tool's own command: `go clean -cache`, `go clean -modcache`, `npm cache clean --force`, `xcrun simctl delete unavailable`, `brew cleanup` | rebuilds itself |
| `deps` | `node_modules`, `target`, `.venv` in repos with no session or file activity for the idle limit (default 14 days, configurable *(author's call)*), whose lockfile is committed | delete the folder | rebuilds itself |
| `agent-versions` | installed agent CLI versions that are not the newest and have no running process (`~/.local/share/claude/versions/*`), unused Codex packages | delete the version folder | rebuilds itself |
| `archived` | Codex `archived_sessions` | per-item only, never `--all` | your data |

Sizes use the existing on-demand size walk with its cap.

## Part 4: early warnings

New alert kinds through the existing inbox and notifier: `low_disk` (free space below `[machine] disk_free_min`, default 10% of the volume or 20 GB, whichever is smaller *(author's call)*) and `memory_pressure` (OS level critical for two consecutive samples). Each alert says what driga can offer ("cleanup can free 9.4 GB", "3 idle sessions hold 4 GB") and clicks through to the Machine view. Existing alert rules apply (repeat interval, batching, off switch).

## Testing

Fixtures for footprint reads, parent-chain grouping and attribution proofs (including a process in a project directory that started before the session, which must not be attributed); cleanup kinds with fake tool commands and restricted PATH; alert planning from recorded samples. Real-machine checks with scratch `DRIGA_*` dirs only.

## Left to the builder

Layout of the Machine sections, the exact Linux footprint source, how candidates are batched in the cleanup view.
