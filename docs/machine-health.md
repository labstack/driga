# Machine health (Parts 1 and 4)

`driga machine --json` and MCP `get_machine` use the same schema 2 view:
`sample`, `top_memory`, `top_cpu`, `mcp_servers`, and the existing `memory` agent-role totals.
Each entry in `machines` has its own rankings; top-level rankings follow the
selected primary machine. Each ranking contains at most ten app groups, with
all their sampled processes. MCP retains its standard `limit`/`truncated`
contract, which can cap nested process entries; the CLI JSON view is uncapped. These are observed processes, not an exhaustive
accounting of kernel memory or unreadable tasks.

The TUI Machine tab (`5`, or `driga top --tab machine`) expands an app with
Right and collapses it with Left. Enter shows process details, including memory
measures and attribution proofs. The web `/machines` page uses keyboard-accessible
native disclosure controls. Both preserve the existing compact table/pane styles.

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

`pileups` contains **every** command with more than one instance under the same
host. One instance under each of several hosts is not a pile-up. Findings sort
by total measured memory, then count, with deterministic command/PID ties.
Each includes original commands, server count, memory and measure, oldest/newest
known start, host PID/name/executable/project/start and `age_ns` (nanoseconds; zero if
start is unavailable), plus advice naming the host to restart. Unknown starts
remain zero timestamps; they are not inferred.

`commands` rolls up server identities machine-wide, including legitimate singles.
Rows show `<label>  <N> servers · <memory> · <H> hosts · <duplicates> duplicates`
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

## Early warnings

`[machine] disk_free_min` accepts an integer byte string or KiB/MiB/GiB suffix,
for example `disk_free_min = "5GiB"`. Its environment override is
`DRIGA_MACHINE_DISK_FREE_MIN`. The default `"0"` means automatic: the smaller
of 10% of the sampled root volume or 20,000,000,000 bytes (decimal 20 GB).
Warnings trigger strictly below the limit; equality ends the episode.
The old `cleanup.low_disk_*` keys remain accepted for config compatibility but
no longer control machine warnings.

The existing `low_disk` episode IDs and state are reused. Existing open items
receive updated text and a Machine click target, without a duplicate item.
Only already-known worktree paths are mentioned as cleanup candidates; driga
does not claim a reclaim size it has not measured. No new cleanup kinds are
implemented.

`memory_pressure` starts after two consecutive, distinct critical OS samples.
Normal, warning, unknown, and failed collection break the streak. State survives
collector restarts. Recovery ends the item while retaining its inbox history.
Acknowledging an item suppresses further notifications for that episode.
When MCP pile-ups exist, the pressure text includes a short count and the
largest group with its host and restart advice. Active unacknowledged episodes
refresh this evidence without changing their identity, timestamp, or throttle.

Both kinds follow `inbox.alerts` (`off` disables them), `alert_repeat`, and the
existing three-second batching window. Repeat throttles are independent per
machine and kind. Unacknowledged active machine warnings can repeat after the
interval; delayed viewer/event passes cannot create extra samples. Health text
survives the titles-only policy, since it contains operational readings rather
than agent transcript text.

Notification clicks use the existing terminal-notifier `driga jump <id>` path.
The stored destination is `machine`. On macOS, the click opens Terminal and runs
the installed executable's `top --tab machine`; on Linux, a manual jump uses
`x-terminal-emulator -e`. This avoids requiring a running web server. As with
existing Linux alerts, notify-send itself has no click action. A click does not
acknowledge the item. No notification is sent by tests; their runners/notifiers
are injected and subprocess PATHs are restricted.
