# Driga

Driga is a local CLI and terminal UI for Claude Code and Codex sessions, project
status, an attention inbox, file overlaps, orphaned helpers, and machine health.
It reads agent metadata and local Git state. It needs no API key and makes no
network requests during collection.

macOS is the supported collection platform. The code builds on Linux; native
process and machine collection there currently return `ErrUnsupported`.
The collector's <50 MB RSS budget is a release target awaiting real measurements.

## Install

Requires Go 1.27.2 or newer and Git on PATH. Once the module is published:

```sh
go install github.com/labstack/driga/cmd/driga@latest
```

From this checkout:

```sh
CGO_ENABLED=0 go build -o driga ./cmd/driga
./driga --help
```

Put the binary on PATH before setting up hooks or the service. Both installations
record its absolute path, so keep that location stable.

## First five minutes

```sh
driga
driga agents
driga projects
driga top
```

The first command shows a summary. Without a collector, read commands collect a
disposable snapshot and display `snapshot · no history`. No persistent database
is written by that collection. A service hint appears at most once a day.
One session in one repository works without configuring roots or a daemon.

Run `driga init` for optional setup: repository roots, text capture, configuration,
service installation, and hooks. Each step can be skipped. Session working
directories always contribute repository discovery; there are no default roots.
`driga top --project .` scopes the viewer to the current project.

For ongoing history and inbox events:

```sh
driga service install
driga doctor
driga inbox --since yesterday
```

Service installation starts the per-user collector. The first import marks older
events as already seen, so historical completions do not fill the inbox.

## Everyday commands

```sh
driga agents --all                 # include sessions ended within 24 hours
driga agents --json --project .
driga agent <session-id>
driga where <project>
driga inbox                       # unacknowledged events; "inbox list" also works
driga inbox --all
driga inbox ack <item-id>          # explicit acknowledgment
driga conflicts
driga orphans                     # "orphans list" also works
driga orphans kill <candidate-id>  # previews the tree and requires y
driga machine
driga agents --watch=2s
```

Data views support `--json`, `--project <name|path|.>`, `--since <duration|yesterday>`,
`--snapshot`, and `--no-color`; `NO_COLOR` is also honored. View JSON has schema 1,
an observation time, collection mode, source freshness, coverage gaps, and data.
`doctor --json` returns diagnostic checks rather than a view envelope.
Use `driga <command> --help` for command-specific flags.

Labels are `working`, `waiting`, `idle`, `stalled?`, `finished`, `dead`, and
`unknown`, with evidence in session details. `stalled?` means suspected inactivity.
Hosted Codex threads use heuristic labels prefixed with `~`; a shared host process
does not establish individual thread liveness. Unknown agent versions are parsed
tolerantly and reported by `driga doctor`.

In the TUI, use `1`–`5` for tabs, Enter for detail, Esc to return, `?` for help,
and `q` to quit. Opening details or issuing JSON queries never acknowledges inbox
items. Use `x`/Space in Inbox or `driga inbox ack`. Without a running collector,
acknowledgments are queued for the next collection. Orphan kills recheck process
identity and the descendant set immediately before signaling; young candidates
are refused. `--force` requires a second confirmation before SIGKILL.

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

Defaults and their purpose:

| Setting | Default | Purpose |
| --- | --- | --- |
| `roots` | none | Discover from sessions; add scanning roots only when wanted. |
| `root_depth` | `3` | Bound discovery below configured roots. |
| `exclude` | `**/node_modules`, `**/.tmp/**` | Avoid dependency and temporary directories. |
| `stuck_after` | `10m` | Suspect a quiet busy/mid-turn session after this interval. |
| `orphan_min_age` | `10m` | Show fresh leaks as young; withhold kill until mature. |
| `capture_text` | `titles` | Require opt-in for prompt/handoff snippets. |
| `no_service_hint` | `false` | Allow a once-daily service hint in snapshot mode. |

Both adapters are enabled by default. Override them with `[adapters.claude]` or
`[adapters.codex]` and `enabled = false`. `open_cmd` optionally configures the TUI's
terminal-opening command using `{cwd}`; otherwise the terminal is detected.

| Data | Default path | Override |
| --- | --- | --- |
| SQLite database and collector state | `~/Library/Application Support/driga/` | `DRIGA_DATA_DIR` |
| Hook/ack spool | `~/.driga/events/` | `DRIGA_SPOOL_DIR` |
| Configuration | `~/.config/driga/config.toml` | `DRIGA_CONFIG` |

Database/spool files use mode `0600`, and their directories use `0700`. Claude
data is read under `~/.claude/`; Codex under `~/.codex/`, or `CODEX_HOME` when set.
The Codex catalog is opened read-only. Local Git commands never fetch.

## Hooks and service

Preview optional integration before applying it:

```sh
driga hooks install --dry-run
driga hooks install
driga hooks status
driga hooks uninstall --dry-run
driga hooks uninstall
```

`--agent claude|codex|all` selects the integration. Installation shows a diff and
asks for confirmation. Existing files are backed up to `*.driga-bak-<timestamp>`.
Claude installation appends commands for Stop, Notification, SessionStart, and
SessionEnd to `~/.claude/settings.json`, preserving existing hooks. Codex
installation sets `notify` in its `config.toml` only when unset; an existing notify
command stays untouched and rollout tailing continues. TOML comments may be lost
when a change is written; the backup retains the original. Uninstall removes only
Driga entries. Hooks append events to the spool; the collector alone writes SQLite.

The service is a per-user LaunchAgent named `run.driga.collector`:

```sh
driga service status
driga service logs -f
driga service stop
driga service start
driga service restart
driga service uninstall
```

Logs are at `~/Library/Logs/driga/collector.log`. `driga collect` runs a foreground
collector; `driga collect --once` writes one collection pass. Only one collector
can hold the lock in a data directory. Idle collection samples processes/machine
every 60 seconds and Git every five minutes. A viewer heartbeat speeds these to
5 seconds, 10 seconds, and 30 seconds respectively. Cwd lookup remains at most
once per minute. Full Git status runs at least every five minutes.

## Extend process signatures

Agent/helper patterns live in
[`internal/signatures/signatures.toml`](internal/signatures/signatures.toml).
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

## Validation and release

The [design](docs/specs/2026-10-18-driga-cli-tui-design.md) §§2a and 15 require
generality across machines and agent mixes. Adapter/derive tuning fixtures live
under `testdata/fixtures/<adapter>/<version>/tune/`. `TestHeldOutFixtures` uses
only `heldout/<scenario>/`, running the full collector snapshot with both actual
adapters and a file-backed fake platform.

The synthetic corpus includes one repo/one agent, 50 repos/150 worktrees/150
agents, untested versions, Linux-style home layouts, crashes with partial lines
and rotated predecessors, and orphan broker trees including a young candidate.
Seeded generation uses fixed time and different identities/layouts from tuning
fixtures. Every set has manually reasoned `expected.json` labels for sessions,
orphans, and conflicts. Those labels are written from the scenario and spec before
validation; they must never be copied from Driga output. A production mismatch
stays failing for maintainer review. Changing thresholds to fit held-out data is
not an acceptable fix.

Fixtures use `{{FIXTURE_HOME}}` for relocatable paths, `scenario.json` for the fixed
clock and layout, `procs.json`, optional `machine.json`, and `mtimes.json` for
timestamps that Git checkouts cannot preserve. `git.fixture` path components
become `.git` only inside a temporary copied HOME. Tests isolate all `DRIGA_*`
paths and `CODEX_HOME`; the corpus and real agent directories are never mutated.
Scrubbed real contributions should follow the same loader format, with human
labels. Multiple adapter/version sets may use the same scenario name.

The `real-second-machine` requirement currently skips with
`missing heldout set: real-second-machine`; it needs a maintainer contribution.
The 50-repo case currently exposes missing cross-worktree file conflicts when
session paths use macOS `/var` aliases and Git returns `/private/var` paths
(§7.3). Its failing expectation is retained for maintainer review.

```sh
go test ./internal/fixturegen ./internal/validate -v
go vet ./...
go test ./...
CGO_ENABLED=0 go build ./cmd/driga
GOOS=linux go build ./...
```

In this sandbox, set `GOCACHE="$PWD/.superpowers/gocache"` and use cached
dependencies with `GOPROXY=off`. To regenerate synthetic inputs explicitly, run
`DRIGA_FIXTURE_OUTPUT="$PWD/testdata/fixtures" go test ./internal/fixturegen -run
TestGenScenarioShapes -count=1` as a single command. This never rewrites
`expected.json`; review any scenario change and its labels separately.

Release checklist:

- [ ] Review and resolve held-out production mismatches against the spec.
- [ ] Add `real-second-machine` and held-out, scrubbed real samples from at least
  three distinct machines, including provenance without personal information.
- [ ] Compare agents against `ps` and agent session lists, and orphans against
  human-labeled process trees on those machines.
- [ ] Pass formatting, vet, tests, a cgo-free build, and Linux cross-build.
- [ ] Have the maintainer run `scripts/footprint.sh` on macOS with representative
  real histories. Record both phase peaks, collector/child/combined RSS, and
  available wakeup readings; sampled peak collector RSS must be <=50 MB.
- [ ] Review combined RSS and average CPU (<1% target) separately on large real
  histories. Five-second sampling can miss brief subprocess peaks.

The footprint script builds a temporary binary, reads the real HOME/configuration,
isolates its database/spool, and runs for ten minutes: five without a heartbeat,
five with one. It samples collector and descendant RSS every five seconds and
reports `top` IDLEW where available. It cleans up its collector and temporary
state on exit. It is a manual acceptance check and is not run by tests.
