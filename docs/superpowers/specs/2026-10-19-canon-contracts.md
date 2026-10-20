# Canon conformance: schema 2 and configuration contracts

This note supersedes earlier Driga JSON/MCP contract examples. v1 is unreleased:
the rename is clean, with no command, tool, field, or enum aliases. README is
maintainer-owned and is deliberately unchanged.

## MCP catalog

| Old | New |
| --- | --- |
| driga_activity | get_activity |
| driga_summary | get_summary |
| driga_agent | get_agent |
| driga_agents | list_agents |
| driga_projects | list_projects |
| driga_where | inspect_project |
| driga_inbox | list_inbox |
| driga_conflicts | list_conflicts |
| driga_orphans | list_orphans |
| driga_machine | get_machine |
| driga_machines | list_machines |
| driga_doctor | inspect_setup |
| driga_cleanup_candidates | list_cleanup_candidates |

`get_flow` is the additional family-detail tool: optional positive `id` selects a
lead or descendant; omission selects the newest live interactive lead. It also
accepts `machine`, `project`, and the same bounded `limit` contract below.

All tools are read-only and safe to retry; inbox reads never acknowledge items.
Tool descriptions explain use, scope, inputs, limits, and result meaning. The SDK
advertises typed input/output schemas. `id` and `project` are required for the
corresponding detail tools. Cleanup `kind` permits `worktree`, `scratch`, or
`history`; omission selects all. `sizes` opts into the existing bounded sizing
scan and never authorizes removal.

Every tool accepts integer `limit`, default 100, range 1–500. It bounds array
entries across the result, including nested families and machine activity
buckets. Arrays are visited in deterministic field order. `truncated` is always
present on MCP envelopes; true explicitly signals omitted entries. There is no
cursor in this pass: narrow `machine`, `project`, or `since`, or increase `limit`.
Counts are full-scope aggregates and are not recomputed from the bounded rows.
Flow nodes' fixed `observed` measurement vectors are not collection entries:
both 60-minute arrays remain complete at every MCP limit. `observed` is null
without retained positive measurements; otherwise it contains integer
`log_bytes_per_min`, `cpu_ms_per_min` (oldest first, current minute included),
UTC `last_growth_at`, and integer `last_growth_bytes`. CPU-only history has a
zero last-growth instant. These are file-metadata and process-counter
measurements, never statements about transcript content. Positive passes only
are summed in `observation_minutes`, retained and pruned alongside event minutes
for 30 days. The first collector pass establishes baselines; truncation,
rotation, and process identity changes reset them without manufacturing activity.
CPU deltas belong only to an unambiguously owned session process; shared hosts
and helper trees are not divided or copied into session CPU measurements.
CLI tables retain their normal complete collection views; machine memory rows
no longer have an implicit top-ten cutoff.

## Fields, values, and units

The [JSON Schema](../../contracts/schema2.schema.json) describes read envelopes and errors.
The read envelope is schema 2:

```json
{"schema":2,"observed_at":"2026-10-19T19:00:00Z","mode":"daemon","freshness":{},"coverage_gaps":[],"data":{}}
```

Owned instants serialize as UTC RFC 3339, retaining nanosecond precision where
available. Internal Go durations stay durations; JSON uses integer milliseconds.

| Old wire field | Schema 2 field / meaning |
| --- | --- |
| age | age_ms |
| last_seen_age | last_seen_age_ms |
| age_seconds | age_ms (converted from seconds) |
| cpu_seconds | cpu_ms (integer cumulative CPU milliseconds) |
| size, pos | size_bytes, position_bytes |
| rss, rss_total, mem, reclaim | rss_bytes, rss_total_bytes, mem_bytes, reclaim_bytes |
| mem_used, mem_compressed, mem_total | corresponding name with _bytes |
| swap_used, swap_total, disk_free | corresponding name with _bytes |
| ts on samples/activity buckets | sampled_at |
| ts on events, completions, file edits, feed rows, spool records | occurred_at |
| first_seen, last_seen | first_seen_at, last_seen_at |
| start_time, proc_start | started_at, proc_started_at |
| oldest_dirty_mtime, mtime | oldest_dirty_modified_at, modified_at |
| from, to on gaps | started_at, ended_at |
| at on summary provenance/audit entries | occurred_at |
| last_connected on fleet viewers | last_connected_at |

Byte quantities stay integer bytes, not formatted display strings. Zero instants
retain the existing zero-time representation. Omission still follows existing
`omitempty` fields; no new clearing/null semantics are introduced. IDs and natural
keys retain their identity and native punctuation.

Owned enum values use snake_case: `same_worktree`, `same_file`,
`same_file_other_worktree`, `worker_finished`, `low_disk`, `needs_review`,
`report_only`, `not_yet_receiving`, `last_event`, `reset_pending`, the enrollment
operations, and adapter diagnostic codes. Session state is `stalled`, with a
separate `heuristic` boolean. `~` and `?` belong to human presentation only.
External agent hook/transcript kinds such as `Stop` and `last-prompt` retain
their protocol spelling. Human tables keep hyphenated status/kind labels.

## CLI framing and exit status

`version --json` and `config path --json` return schema-2 objects with `data`.
`hooks status --json` clearly rejects the unsupported format. Other interactive
or mutation commands without a JSON implementation also reject it before work
starts. Existing supported JSON commands retain their documented data shape. Doctor
failures return one error record with the failed checks in its message, avoiding
a diagnostic array followed by a second JSON object.

Failures in JSON mode write a compact record on stdout and diagnostic text on
stderr, with a nonzero exit status:

```json
{"schema":2,"error":{"code":"usage_error","message":"unexpected argument bogus"}}
```

Stable generic codes are `usage_error`, `runtime_error`, and
`unsupported_output`. Cleanup partial failures use `cleanup_failed` and retain
all per-target outcomes in the same envelope. Messages pass through the existing
secret scrubber.

Exit codes: **0** success (including no-op and a watch interrupted cleanly),
**1** runtime failure, **2** argument/format usage error. Output write errors are
runtime failures; a closed or failed stdout cannot guarantee delivery of its
error record.

`--watch --json` emits compact NDJSON, one complete envelope per line. There are
no table headers, terminal escape sequences, or indentation. A clean interrupt
ends at EOF with exit 0; a runtime failure ends with an error record and exit 1.
`feed tail --after-seq N` uses the feed's exclusive sequence cursor. `--since`
continues to mean a duration or `yesterday` for view commands and is rejected by
feed tail.

Cleanup JSON action data is `{"outcomes":[...]}`. Each target has `id`, `kind`,
and `status`: `completed`, `unchanged`, `failed`, or `cancelled`. Results retain
command plans, trash paths, dry-run information and warnings; errors have code
and message. Bulk ineligible targets and successful dry-runs are `unchanged`.
JSON application requires `--yes`; missing consent is `cancelled`. A failed
target does not discard earlier outcomes. A dry-run does not remove or move a target.

## Persisted data and migration

Goose migration **8**, `internal/store/migration_canon.go`, runs transactionally
on writable store opens. Earlier SQL migrations remain immutable. It rewrites
stored session states/heuristics, inbox/event/machine enum values, owned warning
metadata keys, and retained feed JSON, including nested fields, byte names,
duration units, and UTC instants. It processes feed records in batches of 100;
malformed required payloads abort rather than being discarded. Sequence numbers,
natural keys, integer precision, references and free-form prose are preserved.
The migration intentionally refuses downgrade.

Fleet trust-file loading has an idempotent migration for `last_connected`:
the old key and saved timestamp offsets are atomically rewritten before current-schema decoding. New readers
and writers only expose `last_connected_at`. Peers/viewers serialize UTC times.
Historical SQL column names stay stable; the fleet snapshot projection explicitly
maps the schema-2 wire names to those columns. Schema-1 fleet peers are rejected;
upgrade the unreleased binaries together.

## Presentation and validation decisions

The web console preserves its existing typography, theme tokens, density, and
pane structure. Tail context comes from the same machine's stored session and
project, falling back to session identity when attribution is unavailable.
Tail heights are multiples of an explicit line height. Intake status occupies a
single keyboard-focusable status segment, with timestamps and errors on hover;
project risk annotations sit inline beside the relevant project summary.

Any nonempty `NO_COLOR` disables color, including `0`; an empty value is unset.
Hook speed is tested using the median of five process-CPU measurements, avoiding
scheduler and filesystem delays while retaining the 20 ms CPU budget. The small
empty-input wall-clock assertion was removed; blocked-input and bounded-command
tests retain generous timeout checks. Ack's 2 s production wait has 5 s test
headroom for loaded builders.

## Configuration contract (pass B)

[`internal/config/settings.go`](../../../internal/config/settings.go) is the
authoritative inventory of names, native types, units, defaults, requiredness,
purpose, accepted ranges, and zero semantics. It drives defaults, strict loading,
TOML rendering, service environment forwarding, and the generated root
[`.env.example`](../../../.env.example). Regenerate with
`go generate ./internal/config`; a test compares the committed bytes. Driga never
loads dotenv files. The generated template comments out every assignment.
`.env` and all other `.env*` files are ignored except `.env.example`.

Precedence is **explicit supported flags > nonempty environment > TOML file >
defaults**. `serve --listen` and `fleet enroll --window` override the corresponding
settings; `serve` explicitly enables fleet serving. Related-field validation runs
after those flag overrides. Invalid source values still fail validation even if
a higher-priority source would replace them. Empty environment values are unset.
`config show` emits valid TOML with a `# source: default|file|env` comment before
every value, including values explicitly set equal to a default.

Every scalar key has the mechanical name `DRIGA_` plus its uppercased full TOML
key path, replacing dots/hyphens with underscores. For example, `capture_text`
maps to `DRIGA_CAPTURE_TEXT`, `summaries.enabled` to `DRIGA_SUMMARIES_ENABLED`,
`fleet.listen` to `DRIGA_FLEET_LISTEN`, `intake.otlp` to `DRIGA_INTAKE_OTLP`, and
`cleanup.worktree_idle` to `DRIGA_CLEANUP_WORKTREE_IDLE`. Adapter switches are
`DRIGA_ADAPTERS_CLAUDE_ENABLED`, `DRIGA_ADAPTERS_CODEX_ENABLED`, and
`DRIGA_ADAPTERS_GEMINI_ENABLED`. The registry test checks collisions.

The structured lists `roots`, `exclude`, `orphans.extra_signatures`, and
`cleanup.regenerable` are **file-only**, with no environment separator. Regenerable
directories extend the built-in list with deduplication; other lists replace their
defaults. An explicitly empty regenerable list retains built-ins.

Unknown TOML keys, sections (including empty sections), and adapter names fail,
naming their full key path and file. Unknown names anywhere in the owned
`DRIGA_` environment namespace fail, even when empty. Unrelated names are outside
that namespace. The only namespace extensions are the three path overrides and
these exact internal/test markers, documented in the generated inventory:
`DRIGA_INTERNAL`, `DRIGA_FIXTURE_OUTPUT`, `DRIGA_COLLECTOR_HEAP_CHILD`,
`DRIGA_FLEET_HEAP_CHILD`, `DRIGA_OTLP_TRACES_ADDED`, `DRIGA_JSON_WATCH_CHILD`,
and `DRIGA_TEST_MCP_PROCESS`. There are no wildcard exemptions. Agent-owned
`OTEL_*`, `CLAUDE_*`, `CODEX_HOME`, and similar native names retain their consumer
meaning. Driga delegates Claude credentials to the external CLI and does not
load or log SDK secrets itself.

Durations use Go duration strings (`ns`, `us`/`µs`, `ms`, `s`, `m`, `h`), never
negative, with an upper bound of `2562047h47m16.854775807s` except enrollment,
which accepts `1ns..10m`. Invalid durations and byte sizes fail at load with the
setting/source and allowed range. Byte sizes are strings of unsigned integer
bytes with optional case-sensitive `KiB`, `MiB`, or `GiB`; the final value must
fit `0..18446744073709551615` bytes. Percentages are finite `0..100`.

Zero semantics: `stalled_after=0` selects the existing 10-minute fallback;
`orphan_min_age=0` and `cleanup.worktree_idle=0` remove their age minimums;
`fleet.enroll_window=0` is invalid. Either low-disk threshold at zero disables
low-disk warnings, since the effective threshold is the minimum of bytes and
percentage. `root_depth=0` inspects only the roots. Listener port zero asks the
OS for an ephemeral port. Empty `open_cmd` auto-detects the editor. The optional
summary, fleet, and OTLP integrations remain off by default.

The clean rename is `stuck_after` → `stalled_after` and
`StuckAfter` → `StalledAfter`; there is no alias. Existing TOML files must change
the old key. No automatic config rewrite or fallback to retired spellings occurs.
The `unpair` alias is removed; use `fleet remove`.

Collector settings require restart. CLI and hook invocations reload configuration
each time. A service installation captures the nonempty supported environment
settings (including `XDG_CONFIG_HOME`); changing captured service variables requires
updating/reinstalling its service definition. Internal/test markers are not captured.

## Config and spool locations; installed-machine behavior

`DRIGA_CONFIG` still wins. Otherwise, an absolute `XDG_CONFIG_HOME` selects
`$XDG_CONFIG_HOME/driga/config.toml` on Linux and macOS; unset, empty, or relative
values select `~/.config/driga/config.toml`. Config files are not relocated.
An installation newly opting into XDG should move its file there or explicitly
point `DRIGA_CONFIG` at its existing file. Data directories and existing IDs stay put.

`DRIGA_SPOOL_DIR` still wins and disables automatic legacy migration. Otherwise,
an absolute `XDG_STATE_HOME` selects `$XDG_STATE_HOME/driga/events` on either OS.
The Linux fallback is `~/.local/state/driga/events`; the macOS fallback is
`~/Library/Application Support/driga/events`, next to the native data directory.
Relative XDG paths are ignored. These directories use mode 0700 and newly created
spool files use 0600.

On collector startup, including `collect --once`, under its exclusive writer
lock and before listeners/draining, legacy `~/.driga/events/*.jsonl` files are
copied into the new spool as `legacy-<original-name>`. They never merge with a
same-day file written by new hooks. On the same filesystem, files are hard-linked, synced, and published without
replacement so an in-flight hook append retains its inode. Across filesystems,
copies are streamed, synced, published atomically without replacement, and compared
by digest; old hook writers must have finished before that copy. The collector transfers
the old `spool/<name>` consumed position only when the source inode still matches
and its size has not shrunk. It records the destination inode and position in
the separate `spool/state/` namespace transactionally **before** removing the old
file. A repeated startup resumes interrupted copies/checkpoints without replaying
already consumed records. Destination conflicts or changing sources stop startup
and retain the source. Trailing partial records remain byte-for-byte intact.

Migration supports state directories on another filesystem. Original stored
offsets remain as retry receipts. Subsequent ingestion commits event writes and
destination offset advancement in the same SQLite transaction. Migrated day files
follow the normal 30-day spool retention (cutoff day retained); unknown filenames
are left alone. Migration leaves a short `README` in the old directory. Existing
diagnostic/unknown files there remain available for manual removal; they are not
reinterpreted as events. Installed hook commands invoke `driga hook ...` and embed
no spool path, so **hooks need no reinstall** after a binary upgrade.

## IDs, logging, retention, and scoped Canon exceptions

New machine identities, including an explicitly requested reset, use canonical
lowercase UUIDv7 from the already present `github.com/google/uuid` v1.6.0 library.
The store generates and owns these IDs. Existing machine IDs remain unchanged;
startup performs no fleet re-pairing. Feed epochs remain opaque random tokens.
External session IDs, process identity tuples, certificate fingerprints/device
IDs, content hashes, cleanup candidate keys, and completion/inbox deduplication
keys retain their natural/protocol semantics; these are not random entity IDs.

`hook-errors.log` now uses the same slog text format as `collector.log`, with
native UTC `time`, `level`, `msg`, stable `event=hook_failed`, and scrubbed `err`.
Collector failure logs also use stable event names and `err`. Untrusted strings
are encoded by slog; raw hook payloads are never logged. Hook errors remain
best-effort and cannot fail the invoking agent. Diagnostic output stays off
structured CLI/MCP stdout.

| Owned artifact | Location and retention |
| --- | --- |
| `collector.log` | Linux: absolute `$XDG_STATE_HOME/driga` or `~/.local/state/driga`; macOS: `~/Library/Logs/driga`. Current file ≤10 MiB plus one ≤10 MiB `.1`, overwritten on rotation; no age expiry. Linux services also use the system journal, whose retention is managed by systemd/operator policy. |
| `hook-errors.log` | Resolved spool directory. Same 10 MiB current + one 10 MiB `.1` size policy; no age expiry. Pre-migration diagnostics left in the old directory are historical files, retained until manually removed. |
| `cleanup.log` | Data directory; JSON action records, same 10 MiB current + one 10 MiB `.1` size policy, no age expiry. This is a bounded local action record, not a guaranteed durable compliance audit. |
| `<settings-file>.driga-bak-*` | Beside each agent settings path, mode 0600; keep newest five recognized regular timestamp backups per path after a successful settings write. Never remove the only backup. Unchanged/dry-run/failed settings writes do not prune. No age expiry. |

Explicit scoped exceptions/adoption decisions:

1. **SQLite integer row keys and foreign keys:** retain local integer surrogate
   keys in observation tables. Their identity is scoped by machine; they support
   indexed joins, local ordering, and existing references. Migrating these to UUIDs
   would require an unrelated database/fleet projection rewrite. New machine
   identities use UUIDv7; existing IDs are preserved as Canon requires.
2. **Prebuilt ECharts bundle:** retain the pinned, licensed
   `echarts-6.1.0.common.min.js` instead of `echarts/core` module imports. The
   maintainer explicitly chose no bundler. This pass adds no frontend dependency
   or build system.
3. **Flat TOML entries alongside structured lists/maps:** retain the existing
   native file contract and requested file precedence. All scalars also have the
   required env interface; environment is the preferred distributed-product input.
   This is a scoped exception to Canon's file-only-for-structured-settings rule,
   avoiding a second config system or an unsolicited removal of file settings.
4. **Historical SQL names/storage units:** existing migration files and internal
   columns remain stable; schema-2 projections supply canonical wire names/units.
   No new public naming exception is introduced.
5. **Legacy diagnostic logs:** migration preserves existing unstructured logs
   in the old directory rather than rewriting historical records or overwriting
   current diagnostics. All new hook records use structured, bounded logging.
