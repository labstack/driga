# Driga

Open-source CLI/TUI (Go) that watches every AI coding agent session on a machine — state, project, branch, memory — plus project status, orphaned helper processes, machine health, an attention inbox, and file conflicts.

- Design (approved 2026-10-18): `docs/specs/2026-10-18-driga-cli-tui-design.md`
- Implementation plan: `docs/superpowers/plans/2026-10-18-driga-v1.md` — follow it task by task; its "Global Constraints" apply to every task.

## Working rules

- Implement directly in this session; don't dispatch subagents for implementation.
- One commit at the end of the run (not per task) unless asked otherwise.
- Not curve-fitted: no defaults or rules tuned to the maintainer's own machine (§2a of the design). Thresholds and rules are validated on held-out fixtures, never re-tuned to fit one sample.
- Never touch the real `~/.claude`, `~/.codex`, or data dirs in tests: use `DRIGA_DATA_DIR`, `DRIGA_SPOOL_DIR`, `DRIGA_CONFIG`, and temp HOMEs.
- No destructive actions (killing processes, editing agent configs) during development without asking.

## Commands

- Test: `go test ./...` (race: `go test -race ./...`)
- Build: `CGO_ENABLED=0 go build ./cmd/driga`; cross-check `GOOS=linux go build ./...`
- Run: `go run ./cmd/driga`, `go run ./cmd/driga top`
