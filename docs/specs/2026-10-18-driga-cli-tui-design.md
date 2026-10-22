# Driga CLI/TUI design

This document describes Driga's collection and presentation boundaries. For
current commands and configuration, see [Usage](../usage.md); for machine
attribution, see [Machine health](../machine-health.md).

## 1. Intent

Developers run coding agents in terminals, desktop apps, and editors across
repositories and worktrees. Driga answers who is doing what, where, and what
needs attention, while showing the evidence and its age.

The same collected state serves human CLI/TUI views and structured JSON/MCP
readers. A label is useful only when readers can distinguish observed facts,
heuristics, and missing data.

## 2. Goals and boundaries

Driga combines agent activity, local project state, attention events, file
conflicts, orphan helper candidates, and machine health. Collection must remain
bounded: no filesystem watchers across repositories, no child process per
session, and no background LLM calls by default.

Reads never acknowledge events or perform cleanup. Mutating commands require
explicit intent; process signaling and removal require target validation and
confirmation. Agent configuration is changed only by explicit setup commands.
Full transcripts are not stored.

## 2a. Generality

- No default project roots, editor, terminal, or agent mix. Discover from
  session evidence and the environment, or use explicit configuration.
- State rules follow mechanisms: PID/start identity, process ancestry,
  reparenting, registry evidence, hook events, and log growth.
- Agent/helper signatures are extensible data, not assumptions about one
  developer's tools. Unknown agent versions are reported by diagnostics.
- Validate thresholds and state rules on independent fixtures, including
  synthetic crashes, partial writes, rotations, unknown versions, one-repo
  setups, and many-worktree setups. Expected labels are reasoned from the
  scenario before collection, never copied from Driga output.
- One agent in one repository without a collector is a supported use case.

## 3. Constraints

macOS and Linux platform code sits behind a platform interface. The release
binary is built without cgo. Local collection needs no API key or network
service; local Git commands do not fetch. Optional fleet sharing, summaries,
and Chat have separate opt-in boundaries.

Agent file formats drift between versions. Parsers tolerate partial and unknown
records and expose diagnostics instead of claiming certainty.

## 4. Design choices

Use one collector as the persistent database writer, with many read-only
viewers. Keep collection separate from presentation so CLI, TUI, web, and MCP
agree on labels and freshness. Keep agent invocation separate from collection:
optional LLM features use the operator's installed Claude CLI.

## 5. Architecture

```text
agent files + process/machine samples + local Git + hook spool
                              |
                        collector
                     derive and persist
                              |
                         SQLite WAL
                              |
                    views and freshness
                  /       |       |       \
                CLI      TUI     web      MCP
```

A per-user collector is optional. Without one, readers take a disposable
snapshot and label it `snapshot · no history`; they do not write the persistent
database. Collector exclusivity is enforced with a data-directory lock.

A viewer heartbeat increases collection cadence. Without a viewer, process and
machine samples slow to once per minute and Git scans to every five minutes.
Freshness and coverage gaps remain visible when a source fails or collection
stops. Hooks append bounded events to the spool; the collector imports them.

Fleet peers use explicit pairing, pinned mutual TLS identities, and versioned
snapshots/events. The serving machine controls text sharing and acknowledgment
grants. See [fleet setup](../usage.md#fleet-setup) for trust and listener policy.

## 6. Data sources and adapters

### 6.1 Claude Code

Read session registries and transcript metadata under the configured Claude
home. Hook events improve lifecycle, attention, and completion evidence.
Text extraction follows the capture policy.

### 6.2 Codex

Read rollout metadata and the catalog read-only. Distinguish terminal/headless
sessions from threads hosted by a shared app-server. A shared host PID does not
prove that an individual thread is live; hosted states are heuristic.

### 6.3 Processes

Sample processes through the platform interface, including PID/start identity,
ancestry, command, memory, CPU, and bounded cwd refreshes. Gemini also reads chat
metadata; Copilot uses hooks and process observations; Antigravity currently
uses process observations only.

### 6.4 Machine samples

Sample memory, pressure, swap, load, disk, and process costs. Keep per-minute
observations for history. Persistent observations retain 30 days; disposable
snapshots have a bounded recent window. See [Machine health](../machine-health.md)
for measurement definitions, attribution, and incomplete-sample behavior.

### 6.5 Git

Discover repositories from session working directories and optional configured
roots. Use local Git for worktrees, branch, dirty counts, last commit, and
already-known ahead/behind state. Bound root traversal and avoid fetching.

## 7. Derived state

### 7.1 Agent states

Keep liveness, activity, completion events, and attention requests as separate
facts. Display `working`, `waiting`, `idle`, `stalled?`, `finished`, `ended`, `dead`, or
`unknown` with evidence and observation time. `stalled?` expresses suspected
inactivity, not proof of failure. `ended` identifies normal session termination
or conversation replacement; `dead` requires an open turn at process loss or
crash evidence. `finished` describes unread turn completion, whose inbox event
survives session termination. Hosted Codex heuristics carry a `~` marker.

### 7.1a Session families

Link a supervisor to workers using process ancestry and available session
metadata. A completed worker contributes to its family rather than generating
a separate top-level completion item. Shared-worktree conflicts inside a family
are suppressed; file overlaps remain visible with family context. Reparented
helpers require ownership evidence beyond sharing a directory.

### 7.1b Inbox semantics

Inbox items represent completion events and attention requests, with stable
identities for deduplication. Reads never acknowledge. Explicit acknowledgments
are queued as spool events for the collector; remote acknowledgments require a
peer grant. First import baselines old events so setup does not flood the inbox.

### 7.2 Orphans

Candidates require agent-helper evidence and process ancestry/reparenting or a
dead spawning session. A cwd match alone does not prove ownership. Young
candidates are shown but cannot be killed. App helpers belonging to a live app
are excluded. Verification observes stable identity and descendants over time.

Immediately before signaling, revalidate PID/start identity, descendants, and
external peers. Abort on uncertainty or tree changes. Terminate leaves before
the root; escalation requires an explicit force request and second confirmation.
Never signal self, init, or protected ancestors. Remote candidates must be
handled on their own machine. Memory totals are estimates, not promised reclaim.

### 7.3 Conflicts

Distinguish shared worktrees, the same absolute file, and the same repo-relative
file in different worktrees on the same branch. Current conflicts use recent
edits by live sessions; older overlap is labeled separately. Normalize equivalent
paths before matching.

### 7.4 Where a project stands

Compose project context from captured handoffs, session titles, and local Git
state with source and age. Optional summaries are cached and require explicit
opt-in. Do not turn stale metadata into an assertion about current progress.

### 7.5 Project activity

Group repository and worktree state with associated sessions. Highlight dirty
work, unpushed commits, and inactive worktrees using configured policy and
available timestamps, while retaining the underlying evidence.

## 8. CLI and structured output

Use `driga --help` and command-specific help as the command reference. Views
support project/machine filters and JSON. Schema 2 envelopes include observation
time, mode, source freshness, coverage gaps, and data. Diagnostics have their
own output contract. MCP tools use the same view builders with bounded results.

### 8.1 User journeys

Start with a snapshot, optionally install the collector, then use the inbox for
ongoing history. Read an item's evidence before explicitly acknowledging it.
Inspect cleanup candidates before requesting removal. Use a project filter for
a focused view and explicit pairing for fleet views.

## 9. TUI

The terminal viewer uses the same view data as the CLI. Tabs cover Home, Agents,
Projects, Inbox, Orphans, Machine, Cleanup, Flow, and Chat. Details show evidence;
help lists the current keys. Mutating actions confirm and retain the CLI's
validation rules. Colors respect `NO_COLOR` and `--no-color`.

## 10. Storage

SQLite WAL separates the collector writer from readers. The embedded baseline
creates the schema; pre-release databases must be reset. Sessions, process
observations, inbox events, source health, and per-minute histories retain
their machine identity.
Data and spool files use mode `0600`; private directories use `0700`.

## 11. Stack

Go provides the CLI, collector, and web server. Kong parses commands, Bubble Tea
and Lip Gloss render the TUI, and pure-Go SQLite stores state. Git runs through
its CLI. See [go.mod](../../go.mod) for the dependency versions.

## 12. Configuration

Configuration uses TOML and `DRIGA_*` environment overrides. Roots are empty by
default; agent adapters are enabled by default. Capture defaults to titles.
Fleet serving, OTLP intake, and project summaries require explicit opt-in. See
[configuration](../usage.md#privacy-and-configuration) for settings and paths.

## 13. Privacy and safety

Title capture permits generated titles and paths. Snippet capture is opt-in,
scrubbed, and capped at 200 characters; scrubbing cannot guarantee privacy.
Do not persist full transcripts. Fleet text policy is controlled by the serving
peer. Optional Chat sends explicit questions and permitted context through
Claude's vendor connection; its own history is subject to the vendor's policy.
Hook installers preview changes, request confirmation, and preserve backups.

## 14. Repository layout

`cmd/driga` owns binary entry points. `internal/adapter` parses agent formats;
`platform` samples the OS; `gitscan` reads Git; `derive` computes state; `store`
persists it; `collector` schedules and writes. `view` provides common envelopes
for `cli`, `tui`, `web`, and `mcp`. `hooks` writes/install events and `fleet`
handles paired machines. Scrubbed fixtures live under `testdata/fixtures`.

## 15. Testing

Use adapter fixtures for format drift, derive tests for synthetic state rules,
temporary databases for baseline creation, isolated collector snapshots for
integration,
and fixed-width view snapshots for presentation. Fleet traces cover duplicate
and out-of-order events, epoch changes, stale peers, and atomic replacement.
All agent and Driga homes are temporary; real agent CLIs are never invoked.
Independent expected labels make fixture validation meaningful.
