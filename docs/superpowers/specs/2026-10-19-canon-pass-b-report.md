# Canon pass B report

Implemented in the working tree without committing or pushing. No new dependencies,
CodeGraph initialization, or repository README edits. The existing UUID library
was promoted from indirect to direct use. Pre-existing smoke-test and concurrent
theme/UI changes were preserved.

| Item | Changes |
| --- | --- |
| 1. Strict config | Reject unknown TOML keys/sections/adapters with key + filename and unknown owned env names with an exact internal/test allowlist. Validate types, negative/overflow durations, size overflow, percentage ranges, and related prerequisites. Explicit zero behavior is documented. |
| 2. Env interface | One setting registry drives defaults, env names, loader, TOML output, service forwarding, and generated `.env.example`. Every scalar has a mechanical mapping; lists remain file-only. Empty env is unset. Supported flags override env, file, defaults. `config show` comments every value's source. Dotenv files are ignored except the generated example. |
| 3. Stalled terminology | Clean `stuck_after`/`StuckAfter` → `stalled_after`/`StalledAfter` rename through config, derivation, collection, tests, and docs. No alias. |
| 4. Config location | Honor absolute `XDG_CONFIG_HOME` on Linux/macOS; retain `DRIGA_CONFIG` and `~/.config/driga/config.toml` fallback. |
| 5. Spool location | Use absolute `XDG_STATE_HOME`; fallback to Linux `~/.local/state/driga/events` or macOS `~/Library/Application Support/driga/events`. Collector startup/once migrates legacy JSONL files with checkpointed offsets and separate filenames/namespaces. Hooks resolve the path at invocation. |
| 6. IDs/exceptions | New machine IDs and explicit resets use lowercase UUIDv7; installed IDs and pairings remain unchanged. Scoped SQLite, ECharts, TOML, historical-SQL, and legacy-log exceptions are in the contract note. |
| 7. Logging/retention | Hook failures use UTC slog text, `level/event/err`, scrubbed content, and the existing 10 MiB + one rotation writer. Collector failures share fields. Document all three log policies; successful settings edits retain the newest five backups, including the only backup. |
| 8. Naming follow-up | Removed `fleet unpair`; use `fleet remove`. Reviewed active routes, JSON names, state terminology, IDs, env names, and remaining audit labels against pass A. External protocol spellings and historical migration/report examples remain intentionally unchanged. |

## Installed-machine migration and judgment calls

- Config files are not rewritten or relocated. Rename the old TOML key manually.
  A newly set absolute XDG config root changes file selection; move the config or
  pin its existing path with `DRIGA_CONFIG`. No old-key/path aliases are added.
- Default-spool migration runs under the collector writer lock before listeners
  or ingestion. Legacy files become `legacy-<name>`; same-day new files stay separate.
  Stored positions follow only matching source inodes and non-truncated sizes.
  Destination offsets commit before source unlink; ingestion commits events and
  advanced offsets together. Retry, conflict, partial-line, and new-file coexistence
  tests protect against loss/replay. Same-filesystem hard links retain in-flight
  writers. The tested cross-filesystem copy fallback requires finished legacy hook
  writers, detects changing/conflicting files, and retains sources on failure.
- `DRIGA_SPOOL_DIR` retains its explicit location and old offset namespace and skips
  migration. A short note remains in the old directory; historical diagnostic and
  unknown files remain there until manually removed. Hook reinstall is unnecessary.
- Zero stalled duration selects the existing 10-minute fallback; zero orphan/idle
  durations remove age minima. Zero enrollment is invalid; maximum enrollment is
  10 minutes. Either zero disk threshold disables low-disk warnings.
- Flat TOML remains alongside structured settings as requested; env is preferred
  for distributed deployments. Lists have no env separator. No dotenv autoloading.
- No standalone earlier naming-audit summary was present in the repository. Review
  used the pass-A report/contracts plus a direct scan of the remaining surfaces.
  The contract note records all retained exceptions and their reasons.

## Regression tests

Added strict-key/section/adapter tests; complete scalar env mappings; unknown env
and internal markers; redacted range errors; zero/empty behavior; source provenance
and CLI comments; flag precedence before related validation; generated-example
drift/collisions; platform/XDG paths and service forwarding. Added spool migration
retry/checkpoint, same-day coexistence, stored-offset ingestion, explicit override,
cross-filesystem fallback, collision, partial record, in-flight writer, and migrated
retention checks. Added UUIDv7/legacy identity, structured/rotated hook logging,
runtime hook spool path, UTC log encoding, and newest-five/only-backup tests.

## Final validation

Validation uses `GOCACHE=/private/tmp/driga-canon-go-cache` because the sandbox
cannot write the normal Go cache, and `GOFLAGS=-p=2` also limits nested builds.
Package-scoped iteration used `-count=1`; full suite results are recorded below.

- `gofmt -l .`: clean; `git diff --check`: clean.
- `go vet ./...`: passed.
- `go test -p=2 ./... -count=1`: passed.
- `go test -race -p=2 ./... -count=1`: passed.
- `GOOS=linux go build ./...`: passed.
- `go test ./cmd/driga -run TestSmoke -count=1`: passed.
- No lone timing failure occurred in either full suite; no timing rerun was needed.

No requested implementation work remains beyond the documented scoped exceptions.
The unavailable standalone earlier audit is the only evidence limitation; its
remaining surfaces were reviewed directly against Canon and pass A.
