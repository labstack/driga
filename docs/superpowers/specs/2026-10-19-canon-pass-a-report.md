# Canon pass A completion report

Implemented the requested unreleased-v1 contract changes. No commits or pushes,
no new dependencies, no README changes, and no CodeGraph initialization.

## Renames and contract details

The complete MCP and JSON field rename tables are in the
[schema-2 contract note](2026-10-19-canon-contracts.md). The machine-readable
[JSON Schema](../../contracts/schema2.schema.json) covers envelopes, errors,
freshness, UTC instants, units, and cleanup outcome definitions.

| Old | New |
| --- | --- |
| `feed tail --since N` | `feed tail --after-seq N` |
| `McpCmd` / `Root.Mcp` | `MCPCmd` / `Root.MCP` with explicit `name:"mcp"` |
| `DRIGA_J3_HEAP_CHILD` | `DRIGA_COLLECTOR_HEAP_CHILD` |
| `DRIGA_J4_HEAP_CHILD` | `DRIGA_FLEET_HEAP_CHILD` |
| `internal/collector/j2_test.go` | `internal/collector/event_latency_test.go` |
| `internal/collector/j3_test.go` | `internal/collector/heap_budget_test.go` |
| `internal/fleet/j4_test.go` | `internal/fleet/stream_heap_test.go` |

## Migration

Goose migration 8 (`internal/store/migration_canon.go`) rewrites persisted enum
values, session state/heuristic markers, metadata keys, and nested retained-feed
JSON in bounded batches. It retains sequence numbers, IDs, references, prose,
timestamp precision, and integers above 2^53. It is transactional and refuses
downgrade. Existing SQL migrations are unchanged. Fleet viewer trust files have
a separate idempotent atomic key/UTC migration.

## Tests added or expanded

- Schema-2 units, byte names, UTC timestamps, CPU-time round trips and JSON Schema validation.
- CLI JSON success, usage/runtime failure, doctor failure framing, unsupported modes,
  one-record cleanup failure, feed cursor flag, and a real child-process NDJSON watch.
- MCP limit/default/max/invalid-input behavior and cleanup enum discovery over the SDK.
- Old-store migration including nested JSON, heuristic flags, UTC precision and large integers.
- Trust-file migration, repeat loading, current key serialization and UTC output.
- Cleanup typed per-target outcomes and completed removal surviving a later prune failure.
- Empty/nonempty NO_COLOR behavior.
- Tail session/project fallback and inline project risk annotation.
- Existing fixtures retain heuristic expectations using a separate boolean.

## Judgment calls

- Every MCP tool accepts limit, including detail tools with nested collections. It is a
  shared array-entry budget (default 100, maximum 500); truncated signals omissions.
  There is no cursor; tools explain how to narrow scope or increase the limit.
- Internal Go durations and SQL units remain suitable for calculations/storage; JSON
  serialization converts durations to integer milliseconds and instants to UTC.
- External agent protocol names retain their native spelling. Human renderers retain
  friendly hyphenated enums and stalled?/heuristic punctuation.
- hooks status rejects --json with usage exit 2. Version and config path emit JSON.
  Other interactive commands without JSON support reject the format before acting.
- Cleanup JSON requires --yes or --dry-run. Bulk ineligible targets and dry-runs are
  unchanged; missing consent is cancelled. Completed results survive later errors.
- Watch interruption ends with clean EOF and exit 0; runtime failure emits an error record.
- Hook latency uses median process CPU time across five runs. Small empty-input timing
  assertions were removed; bounded-input and command cancellation checks remain.
- The existing console theme/layout was retained. Synthetic 200-event browser verification
  confirmed full visible tail lines, session/project context, compact intake status, and
  inline worktree annotations. Both temporary preview servers were stopped.
- The normal Go cache was denied by the filesystem sandbox during vet; final checks use
  GOCACHE=/private/tmp/driga-canon-go-cache, with package parallelism limited to two.

## Verification

All required checks passed on the final code, using the isolated Go cache and
GOFLAGS=-p=2 for nested builds:

- gofmt reports no files; git diff --check is clean.
- go vet ./...
- go test -p=2 ./...
- go test -race -p=2 ./...
- GOOS=linux go build ./...
- go test ./cmd/driga -run TestSmoke

Both full suites were also run with -count=1 during verification. No lone timing
failure occurred in the final runs; the one-rerun rule was not needed. Console
visual QA used synthetic data only, and both preview servers and their browser
tab were closed.

## Anything undone

Nothing remains undone in the requested scope. README remains intentionally untouched.

## Files changed

The inventory includes new files and the three clean renames; no staging is required.

```text
R internal/collector/j2_test.go -> internal/collector/event_latency_test.go
R internal/collector/j3_test.go -> internal/collector/heap_budget_test.go
R internal/fleet/j4_test.go -> internal/fleet/stream_heap_test.go
M cmd/driga/smoke_test.go
M docs/research/2026-10-18-fleet-transport-evaluation.md
M docs/specs/2026-10-18-driga-cli-tui-design.md
M docs/superpowers/plans/2026-10-18-driga-v1.5.md
M docs/superpowers/plans/2026-10-18-driga-v1.6.md
M docs/superpowers/plans/2026-10-18-driga-v1.md
M docs/superpowers/specs/2026-10-18-driga-v1.5-design.md
M docs/superpowers/specs/2026-10-18-driga-v1.6-design.md
M docs/superpowers/specs/2026-10-19-driga-v1.7-design.md
M internal/adapter/claude/backlog_test.go
M internal/adapter/claude/claude.go
M internal/adapter/claude/claude_test.go
M internal/adapter/claude/transcript.go
M internal/adapter/codex/backlog_test.go
M internal/adapter/codex/catalog.go
M internal/adapter/codex/codex.go
M internal/adapter/codex/codex_test.go
M internal/adapter/codex/rollout.go
M internal/adapter/gemini/gemini_test.go
M internal/adapter/gemini/jsonl.go
M internal/adapter/gemini/legacy.go
M internal/cleanup/cleanup.go
M internal/cleanup/cleanup_test.go
M internal/cleanup/history.go
M internal/cleanup/worktree.go
M internal/cli/act_test.go
M internal/cli/ask.go
M internal/cli/ask_test.go
M internal/cli/cleanup.go
M internal/cli/cleanup_test.go
M internal/cli/config.go
M internal/cli/dev.go
M internal/cli/doctor.go
M internal/cli/doctor_test.go
M internal/cli/feed.go
M internal/cli/fleet_pair.go
M internal/cli/hook_test.go
M internal/cli/hooks.go
M internal/cli/machines_test.go
M internal/cli/mcp.go
M internal/cli/read.go
M internal/cli/root.go
M internal/collector/agy_test.go
M internal/collector/cleanup.go
M internal/collector/cleanup_test.go
M internal/collector/fleet_test.go
M internal/collector/pass.go
M internal/collector/remote_ack_test.go
M internal/derive/conflicts.go
M internal/derive/conflicts_test.go
M internal/derive/inbox.go
M internal/derive/inbox_test.go
M internal/derive/intake_test.go
M internal/derive/state.go
M internal/derive/state_test.go
M internal/feed/decode.go
M internal/feed/feed.go
M internal/fleet/apply.go
M internal/fleet/apply_test.go
M internal/fleet/control.go
M internal/fleet/identity.go
M internal/fleet/manage.go
M internal/fleet/pair.go
M internal/fleet/puller.go
M internal/fleet/puller_test.go
M internal/fleet/server.go
M internal/fleet/snapshot.go
M internal/fleet/snapshot_stream.go
M internal/fleet/trust.go
M internal/mcp/cleanup_test.go
M internal/mcp/server.go
M internal/mcp/server_test.go
M internal/model/model.go
M internal/platform/platform_test.go
M internal/store/store.go
M internal/tui/app.go
M internal/tui/app_test.go
M internal/tui/cleanup.go
M internal/tui/cleanup_test.go
M internal/tui/design_test.go
M internal/tui/detail.go
M internal/tui/fleet.go
M internal/tui/fleet_test.go
M internal/tui/header.go
M internal/tui/home.go
M internal/tui/home_test.go
M internal/tui/tabs.go
M internal/validate/validate_test.go
M internal/view/activity.go
M internal/view/builders.go
M internal/view/dashboard.go
M internal/view/fleet.go
M internal/view/polish_test.go
M internal/view/render.go
M internal/view/style.go
M internal/view/tail.go
M internal/view/verified_test.go
M internal/view/view.go
M internal/view/view_test.go
M internal/view/workers_test.go
M internal/web/pages.go
M internal/web/polish_test.go
M internal/web/static/app.css
M internal/web/static/app.js
M internal/web/templates/dashboard.html
M internal/web/testdata/console.js
M testdata/fixtures/claude/2.1.93/heldout/one-repo-one-agent/machine.json
M testdata/fixtures/claude/2.1.93/heldout/one-repo-one-agent/procs.json
M testdata/fixtures/fleet/heldout-v1/heldout/fleet-traces-synthetic/expected.json
M testdata/fixtures/fleet/heldout-v1/heldout/fleet-traces-synthetic/trace.json
M testdata/fixtures/mixed/broker-layout/heldout/orphan-broker-tree/machine.json
M testdata/fixtures/mixed/broker-layout/heldout/orphan-broker-tree/procs.json
M testdata/fixtures/mixed/broker-layout/heldout/orphan-broker-tree/scenario.json
M testdata/fixtures/mixed/crash-formats/heldout/crash-partial-lines/machine.json
M testdata/fixtures/mixed/crash-formats/heldout/crash-partial-lines/procs.json
M testdata/fixtures/mixed/future-formats/heldout/untested-versions/expected.json
M testdata/fixtures/mixed/future-formats/heldout/untested-versions/machine.json
M testdata/fixtures/mixed/future-formats/heldout/untested-versions/procs.json
M testdata/fixtures/mixed/heldout-v1/heldout/linux-vm-synthetic/machine.json
M testdata/fixtures/mixed/heldout-v1/heldout/linux-vm-synthetic/procs.json
M testdata/fixtures/mixed/heldout-v1/heldout/many-repos-many-worktrees/expected.json
M testdata/fixtures/mixed/heldout-v1/heldout/many-repos-many-worktrees/machine.json
M testdata/fixtures/mixed/heldout-v1/heldout/many-repos-many-worktrees/procs.json
M testdata/fixtures/mixed/linux-layout/heldout/linux-paths/expected.json
M testdata/fixtures/mixed/linux-layout/heldout/linux-paths/machine.json
M testdata/fixtures/mixed/linux-layout/heldout/linux-paths/procs.json
A docs/contracts/schema2.schema.json
A docs/superpowers/specs/2026-10-19-canon-contracts.md
A docs/superpowers/specs/2026-10-19-canon-pass-a-report.md
A internal/cli/contracts_test.go
A internal/cli/feed_contract_test.go
A internal/cli/watch_contract_test.go
A internal/feed/json.go
A internal/fleet/json.go
A internal/fleet/trust_migration.go
A internal/fleet/trust_migration_test.go
A internal/jsonwire/marshal.go
A internal/mcp/limits_test.go
A internal/model/json.go
A internal/store/canon_migration_test.go
A internal/store/migration_canon.go
A internal/view/contracts_test.go
A internal/view/json.go
A internal/view/schema_test.go
A internal/web/canon_polish_test.go
```
