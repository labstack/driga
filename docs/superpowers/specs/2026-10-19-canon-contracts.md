# Canon conformance pass A: schema 2 contracts

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
