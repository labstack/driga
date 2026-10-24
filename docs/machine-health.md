# Machine health

`driga machines show --json` and MCP `get_machine` use the same schema 2 view:
`sample`, `top_memory`, `top_cpu`, `mcp_servers`, and the existing `memory` agent-role totals,
plus `agent_share`, `session_costs`, `agent_processes_without_session`,
`likely_agent_work`, and `other_apps`. All additions keep schema 2. The old
rankings and the trend state value `"leaking"` remain compatible.
Top-level rankings follow the selected primary machine. `machines` contains
only additional hosts, avoiding a duplicate of the primary report; it is omitted
for one host. This documented schema-2 collection change preserves field types.
Default evidence lists contain at most ten rows and three nested process,
command, install, or host-trend entries per row. `truncated` marks omitted entries;
whole-machine totals remain complete. `trend_since` gives the default 24-hour
query boundary. Use `driga machines show --full` or MCP `get_machine` with `full: true`
for retained history and uncapped evidence lists. MCP additionally retains its
standard envelope `limit`/`truncated` contract. These are observed processes, not an exhaustive
accounting of kernel memory or unreadable tasks.

The TUI Machines tab (`2`, or `driga top --tab machines`) expands an app with
Right and collapses it with Left. Enter shows process details, including memory
measures and attribution proofs. The web `/machines` page uses keyboard-accessible
native disclosure controls. Both preserve the existing compact table/pane styles.

## Agents first

Every sampled non-zombie process is placed exactly once, in this order:

1. **Session:** the existing parent-chain proof reaches a live session with a
   matching PID/start identity, or a unique most-specific project cwd matches a
   process started after that session. Ambiguous cwd matches prove nothing.
2. **Agent without a session:** the process or an ancestor has a signature naming
   an agent, or stored agent evidence, in an interactive, headless, host, service,
   or helper role. Role-only `desktop-app`, `generic-mcp-launcher`, and
   `driga-internal` matches are insufficient.
3. **Likely agent work:** an MCP server is the process or an ancestor below the
   nearest non-agent app. Evidence is the parsed package/command name containing
   the case-insensitive token `mcp` at a word, hyphen, or underscore boundary, or
   the existing `generic-mcp-launcher` signature. Incidental argument paths are
   not evidence. These processes group by their shared host app and display
   **shared with editor**. The host's own memory stays in Other apps. A plain
   editor-spawned Node worker or language server remains Other apps.
4. **Other apps:** everything else, in one totals-only bucket.

Parent walks stop on cycles, missing/zombie parents, or a parent started after
its child, so stale PIDs cannot prove ancestry. No new process scans or
per-process reads are added. Rules use mechanisms and signature data, never
machine-specific product lists or tuned memory/process-count thresholds.

`agent_share.memory_bytes`, `cpu_percent`, and `processes` count the first two
buckets; `likely_memory_bytes` is separate. `machine_memory_bytes` is the OS
sample's `mem_used_bytes`. `memory_measure` labels the proven total, and
`mcp_servers` sums the existing MCP command roll-ups' counts (which can include
helper processes). Proven memory plus likely memory plus `other_apps.memory_bytes`
equals the full sampled process memory, not necessarily OS memory in use.

`session_costs` includes each proven live session and all attributed helpers,
with memory/CPU totals and processes ordered by memory. Sessions sort by cost;
the surfaces show the top ten. `started_from` is the session root's nearest app
bundle name without `.app`, omitted when unavailable. It appears only in the
Machine view and JSON for now. Proofs display as **started by** for `parent_chain`
and **in project, started later** for `cwd_started_after`.
`agent_processes_without_session` groups by process name. `likely_agent_work`
groups by shared host app. `other_apps` has ID `other`, name `Other apps`, totals,
and an empty process array. Empty bucket arrays serialize as `[]`.

The summary leads with proven agents' memory against machine memory, optional
likely memory, CPU, process/MCP counts, pressure, swap, and free disk. Next come
Sessions by cost, Agent processes without a session with MCP roll-ups/pile-ups
and observed-growth markers, optional Likely agent work, Other apps, machine
vitals, and Agent memory by role. Web/TUI Other apps starts collapsed and expands
to the existing top-ten memory/CPU rankings; static CLI text prints the nested
rankings. Older peers without buckets retain their supplied rankings, using
available top-memory totals as a fallback rather than claiming full accounting.

Deferred: outbound AI-service connection evidence and user “treat as agent”
marks; editor session stores (VS Code `chatSessions`, Cursor `state.vscdb`, and
Windsurf/Cursor hooks); Xcode's separate Claude transcript directory; pty-host
terminal attribution; live signed-in Copilot editor testing; launch-surface
labels in agent lists, Flow, and alerts. These need separate adapter work or
collection data beyond the current sample.

## Measurements and proof

On macOS, the existing process scan reads `ri_phys_footprint` through the
cgo-free `proc_info` syscall, call number 9 (`PROC_INFO_CALL_PIDRUSAGE`),
flavor 2 (`RUSAGE_INFO_V2`, 176 bytes). The footprint includes compressed memory.
It requests no task port or elevated rights. Denied or exited processes fall
back to the existing RSS reading. The measure is labelled `physical_footprint`
or `rss`; an app containing both is labelled `mixed`.

The ABI follows Apple's [libproc wrapper](https://github.com/apple-oss-distributions/xnu/blob/main/libsyscall/wrappers/libproc/libproc.c),
[call selectors](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/proc_info_private.h)
and [rusage layout](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/resource.h).
System compressed bytes come from `vm_stat`'s physical compressor pages and its
reported page size. Pressure comes from `kern.memorystatus_vm_pressure_level`:
its public values are NOTE bits 1/2/4 (normal/warning/critical), rather than the
kernel's internal enumeration; see Apple's [sysctl conversion](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_memorystatus_notify.c).
The existing free-memory percentage remains separately available.

Linux uses sampled RSS plus `VmSwap` from `/proc/<pid>/status`, labelled
`rss_swap`. PSI remains available as `psi_mem_some10`. Linux PSI does not define
a categorical critical level; `pressure_level` is `unknown`, and driga does not
invent a threshold or send a critical-pressure warning from PSI alone.

`memory_bytes` is the memory amount used for app ranking. Per-process
`rss_bytes`, `footprint_bytes` and `swap_bytes` retain the individual readings.
App CPU sums the collector's existing process deltas (100% is one core).
The first sample after a collector restart can have zero CPU deltas.

App grouping walks the sampled parent chain to the nearest enclosing `.app`
bundle, using the same bundle rule as terminal alert jumps. Nested helper
bundles retain their nearest bundle. Processes outside bundles group by
agent signature where available, otherwise executable basename. A bare version
filename keeps its version: `claude 2.1.304` and `claude 2.1.303` remain separate
groups. Unclassified version executables use their nearest meaningful parent
directory, skipping `versions`, `releases`, and `bin`. Missing executable
identities without a matching signature are grouped as `unknown`.

Attribution is computed from the same snapshot, never from remembered orphan
ownership. A live session's PID must still match its recorded process start.
Proof is either `parent_chain`, including the session process itself, or
`cwd_started_after`: the process cwd is inside that live session's project and
its start is strictly after the session start. Known repo/worktree metadata
provides the checkout root when the session started in a subdirectory. A pre-existing dev server is
unattributed. If several sessions match the same most-specific project, driga
leaves the cwd match unattributed. Parent proofs take precedence. Cwd refreshes
reuse the sampled PID set and the existing minute cadence; no additional process
scan or disk-size walk is introduced.

## MCP servers

The Machine tab/page and `mcp_servers` JSON field compare child commands under
their nearest agent host (interactive, headless, app-server, or managed service).
Wrappers are traversed, but nested agent hosts own their own descendants.
Cycles, zombies, missing parents, and impossible parent start times cannot
prove ownership. No live-process lookup or additional process scan is used.

`pileups` contains commands with more than one instance under the same
host. One instance under each of several hosts is not a pile-up. Findings sort
by total measured memory, then count, with deterministic command/PID ties.
Each includes original commands, server count, memory and measure, oldest/newest
known start, host PID/name/executable/project/start and `age_ns` (nanoseconds; zero if
start is unavailable), plus advice naming the host to restart. Unknown starts
remain zero timestamps; they are not inferred.

`commands` rolls up server identities machine-wide, including legitimate singles.
Rows show `<label>  <N> servers · <memory> · <H> hosts · <duplicates> duplicates`
when the parsed command name passes the MCP token rule; otherwise they say
`<N> helper processes`
and sort by memory. Labels come from npm packages/bins, versioned executable
paths, app bundles, or project build paths. `duplicate_count` sums the count
beyond one server per host; `pileup_count` counts affected hosts. `host_count`
counts distinct owning agent hosts. Each entry in `machines` has its own report;
the top-level report follows the selected primary machine, never combining
unrelated machine PIDs.

Package identities group across Node installations, retaining package scopes
and server arguments (including project paths). Other commands retain executable
paths and arguments, including versions. `command` is a deterministic original
command; `commands` retains every distinct original command and `installs` lists
runtime and entrypoint paths in details and JSON.

A launcher with exactly one live child is collapsed only when its target and
server arguments match the child, with valid ancestry under the same host. This
covers npm/npx exec and Node bin shims. A pair counts as one server, includes both
memory footprints and command lines, and sets `includes_launcher` (displayed as
“includes launcher”). Identical parent/child commands remain separate workers.

Temp roots (`/tmp`, `/private/tmp`, `/var/tmp`, and macOS
`/var/folders/.../T`) and explicit port options/endpoint ports are normalized;
temporary script/JAR entrypoint basenames are retained. Available quoting is
honored; malformed quoting is skipped. Process tables expose strings rather
than lossless argv, so boundaries lost by the OS cannot be recovered.
All non-agent descendant commands participate, including wrapper processes and
entrypoints without an MCP marker; this is evidence of duplicate commands under
an agent host, not proof that each child speaks MCP. Report-only `mcp_server`
patterns in the signatures file give Maestro's Java main and codegraph Node
entrypoints readable names when structural labels are unavailable. Other commands
use executable/bin names and subcommands. These patterns confer no orphan or
cleanup eligibility.

Advice is to restart the named host app/session and scope the MCP server per
project. This feature never signals a process. No cleanup action is added.

### Observed trends

The collector observes the first successful process sample of each minute,
reusing the existing scan and logical server/launcher grouping. `mcp_groups`
interns machine, host PID/start, and command key once; `mcp_labels` interns
host/command display strings. `mcp_states` references their integer IDs, so
metadata changes also preserve the labels at each observation. States retain exact server count,
measured footprint, and logical server PID/start identities. An unchanged state
extends its last observation timestamp in place and writes no new state row.
A state change or UTC hour boundary starts a new run (an hourly heartbeat).
`mcp_clock` stores one timestamp per successful observation shared by all groups;
it preserves exact first/last times when a query window cuts a run or collection
has gaps. Scans feed run endpoints to the trend builder: repeated intermediate
states cannot change starts, ends, identity completeness, or rate math. Missing
commands receive one zero sample, including when their host exits. Failed scans
and machine-only passes add no process evidence. Restarting a host starts a new
history, even if its PID is reused; restarting the collector preserves history.

`mcp_servers.trends` reports up to ten host/command histories from 24 hours by
default; `--full` reports every retained history. Each command
and pile-up also has `trends` for its hosts. Fields include `started_count`,
`ended_count`, `net_growth`, `first_sample`, `last_sample`, `rate_per_hour`, and
`identity_complete`. `resolution_seconds` is 60 for minute detail, or 3600 when
the history includes hourly retained endpoints. The first retained sample is a baseline: processes already
present do not count as additions. Subsequent membership changes count starts
and ends, so replacing a server at a flat count is still **churning**.
**Observed growth** (JSON state `"leaking"`) means at least two distinct observed additions and no observed ends,
with known host and server starts throughout the window. One addition is not
enough. Other histories are **steady**; incomplete identities are labelled in
details and cannot establish observed growth. These are sampled observations: processes
that start and end between minute samples are outside this evidence.

The rate is observed additions divided by the time between first and last
samples, never the host's lifetime or time since the reader opened. Machine
rows show `observed growth ↑ N/h since HH:MM`, `churning`, or `steady`; details give
the full timestamps and counts. Command rollups select the largest growing
host's marker, otherwise churning or steady; they never sum unrelated hosts'
rates or windows. Critical-pressure advice names the largest observed growth by measured
footprint, with its cadence and explicit observed window.

Retention keeps full change detail for at least 24 hours (rounded outward to
the hour), then the last observed state per UTC hour and host/command for up to
30 days. Older trends describe those hourly endpoints; starts/ends between
endpoints can be lost and their rates/classification reflect only retained
evidence. `--full` labels this resolution in text and JSON. Disposable snapshots
keep the current minute plus 59 preceding minutes. Pruned evidence is not
carried forward into rates or classification. A fresh snapshot has only a
baseline and cannot establish observed growth. Every history scan has time
bounds: default reports and critical memory-pressure advice read the last 24
hours; `--full` reads at most 30 days. Reads stream endpoints rather than loading
all retained rows into memory. These local
observations are not added to the fleet wire protocol; peer reports only show
trends when that machine has stored sample evidence.

## Early warnings

`[machine] disk_free_min` accepts an integer byte string or KiB/MiB/GiB suffix,
for example `disk_free_min = "5GiB"`. Its environment override is
`DRIGA_MACHINE_DISK_FREE_MIN`. The default `"0"` means automatic: the smaller
of 10% of the sampled root volume or 20,000,000,000 bytes (decimal 20 GB).
Warnings trigger strictly below the limit; equality ends the episode.

The existing `low_disk` episode IDs and state are reused. Existing open items
receive updated text and a Machine click target, without a duplicate item.
Only already-known worktree paths are mentioned as cleanup candidates; driga
does not claim a reclaim size it has not measured. No new cleanup kinds are
implemented.

`memory_pressure` starts after two consecutive, distinct critical OS samples.
Normal, warning, unknown, and failed collection break the streak. Unknown or failed
readings do not clear an existing episode. State survives
collector restarts. Recovery ends the item while retaining its inbox history.
Acknowledging an item suppresses further notifications for that episode.
Pressure text leads with the agents' proven share of OS memory in use and MCP
count. With no proven agent memory it says agents hold none and other apps are
the cause. Likely memory is never silently included in the proven amount.
When MCP pile-ups exist, the pressure text includes a short count and the
largest group with its host and restart advice. Active unacknowledged episodes
refresh this evidence without changing their identity or timestamp.
Growth evidence is labelled `Largest observed growth:`; no new growth alert is added.

Both kinds follow `inbox.alerts` (`off` disables them). Each condition notifies
once per episode, including across collector restarts and metadata pruning;
only a confirmed recovery re-arms it. `alert_repeat` applies to session alerts.
Health items arriving in one committed batch coalesce per machine, even when
their original timestamps differ. Summaries have no single click destination.
Health text survives the titles-only policy, since it contains operational
readings rather than agent transcript text. Banner text is short; the inbox
keeps the detailed explanation and uses decimal GB/MB for sizes.

On macOS, Driga.app sends native UserNotifications with Driga's name and icon.
A local single-item notification click runs `driga inbox jump <id>` through its
delegate while the collector is running. Unbundled builds have no macOS alerts.
The stored destination is `machine`. On macOS, the click opens Terminal and runs
the installed executable's `top --tab machines`; on Linux, a manual jump uses
`x-terminal-emulator -e`. This avoids requiring a running web server. As with
existing Linux alerts, notify-send itself has no click action. A click does not
acknowledge the item. No notification is sent by tests; their runners/notifiers
are injected and subprocess PATHs are restricted.
