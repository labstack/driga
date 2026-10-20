## What changed

<!-- Two to four sentences. The change itself, not the diff. -->

Closes #

## Why

<!--
The constraint that forced this shape, and what the rejected alternative cost.
If the reasoning outlives the review, it belongs in a docs/ design document.
-->

## Risk and rollout

<!--
Driga runs as a long-lived collector on the user's machine and edits agent
settings on request. Answer for this change:
  - new migration in internal/store/migrations/? It must be additive and run
    against an existing database; say what a downgrade sees.
  - does it change what is written to ~/.claude, ~/.codex or other agent
    settings? Install must preview, ask, back up and round-trip every byte.
  - does it read or store more text? Capture is bounded by capture_text and
    fleet.share_text in every surface, alert and export.
  - can it signal or kill a process? Only processes Driga started, after
    re-verifying identity.
Write "None" for a change that cannot affect a user's machine or data.
-->

None

## Contract changes

<!--
Anything outside this change depends on:
  - schema 2 JSON (docs/contracts/schema2.schema.json), CLI flags or exit codes
  - MCP tool names, arguments or results
  - config keys or DRIGA_* environment variables (.env.example)
  - the fleet wire format or hook payloads
Write "None" if nothing crosses a boundary.
-->

None

## Verified

<!-- Commands run and where. Tests that need macOS process access cannot run in a sandbox; say so. -->

- [ ] `go test ./...` and `go test -race ./...`
- [ ] `GOOS=linux go build ./...`
