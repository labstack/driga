# Contributing to Driga

Driga watches AI coding agent sessions on a developer's machine. It reads agent
histories and process data, and it can signal confirmed orphan process trees, so a change
that is merely plausible is not good enough: say how you know it is correct.

## Licensing and the CLA

Driga is licensed under the [Apache License, Version 2.0](LICENSE).

Before your first pull request can be merged, you must accept the
[Contributor License Agreement](CLA.md). You keep the copyright in your work;
the CLA grants LabStack LLC the rights needed to distribute it, including under
future commercial licensing. An automated check will prompt you on your first
pull request, and will not ask again afterward.

If you cannot accept the CLA, you can still file issues, reproduce bugs, and
review pull requests. Those are genuinely useful and carry no agreement.

## Before you write code

For anything beyond a typo or an obvious bug fix, open an issue first. The
design is described in [docs/specs/](docs/specs/); a change to what Driga reads, stores, shares or
signals is a product decision, and it is cheaper to settle before the code
exists.

## Rules for changes

- No defaults or rules tuned to one machine. Thresholds and rules are validated
  on fixtures that are not the sample they were derived from.
- Tests never touch the real `~/.claude`, `~/.codex`, `~/.copilot` or `~/.gemini` or Driga data
  directories. Use `DRIGA_DATA_DIR`, `DRIGA_SPOOL_DIR`, `DRIGA_CONFIG` and a
  temporary `HOME`. Tests never run real agent CLIs.
- JSON output and the fleet wire format change additively within a schema.

## Setting up

Use Go (the version in [go.mod](go.mod)), Git and [just](https://just.systems) on PATH:

```sh
just build        # builds ./bin/driga, which is not on PATH
./bin/driga --help
just install      # deliberately copies it to ~/.local/bin
```

### The app's UI

The app's page and the panels MCP App are built from `ui/` with [Bun](https://bun.sh):
`just ui` installs the pinned packages, type-checks and writes both bundles into
`internal/web/app` and `internal/mcp/apps`. The bundles are committed, so a Go
build needs no JavaScript toolchain. Run `cd ui && bun run test` for the UI tests.

`just dev` develops the UI in a browser at http://127.0.0.1:5173/app/: Vite
hot-reloads it, and `driga dev serve` answers the API on 127.0.0.1:47801 from
the data directory your environment names, restarting on Go changes when
`watchexec` is installed. It is the app without its window: no native dialogs,
menus, tray or notifications, and the browser's own confirm instead.

The UI follows LabStack's Canon: Mantine components themed through
`ui/src/theme`, Phosphor icons, TanStack Table for tables and one ECharts
wrapper for charts. Its colors come from the console palette: after changing a
color in `internal/view`, run `DRIGA_WRITE_THEME=1 go test ./internal/view` to
regenerate `ui/src/theme/palette.json`. Style sheets name theme variables, never
colors. `@labstack/ui` is a private package this public repository cannot
install, so Driga composes its status badge and stat tile from Mantine instead.

## Verify

```sh
just check        # gofmt, go mod tidy, vet (native and Windows), generated files, tests
just race         # the suite under the race detector
just ci           # what the Linux CI job runs: check, race, govulncheck, actionlint, release snapshot
```

`just ci` also needs `govulncheck`, `actionlint`, `shellcheck` and `goreleaser`.
Tests run through `scripts/isolated.sh`, which gives them a disposable home and
Driga data, so they never see your real agent configuration.

Tests that read macOS processes fail with `operation not permitted` inside a
sandbox; run them on the host.

## Commits and pull requests

- Write imperative commit subjects prefixed with a type such as `fix:`,
  `feat:`, `docs:`, `test:`, or `chore:`.
- Keep each pull request focused and explain why the change is needed.
- State what you ran and any skipped or failing checks.
- Scrub personal paths, session text, and credentials from contributed fixtures.

## Releasing (maintainers)

```sh
just release      # runs scripts/release.sh
```

Driga is in alpha. The script creates the next `vYYYY.M.REVISION-alpha` tag,
starting at revision zero each UTC month. GitHub marks it as a prerelease;
Homebrew and Scoop publish the same alpha version.

This requires a clean, checked, up-to-date `main`. It checks formatting, runs
`go vet ./...` and `go test ./...` with isolated agent and Driga state, then
atomically publishes a metadata-only fast-forward release commit plus its tag
to `origin`. The release identity needs permission to fast-forward `main`.
A branch policy that refuses the update aborts publication without leaving a
tag behind. Wait for the previous release run to finish before cutting another.

The tag workflow reuses CI, signs and notarizes macOS binaries, publishes the
GitHub Release, and updates Homebrew and Scoop. Configure the required secrets
and publication targets in [docs/release.md](docs/release.md). Repair a failed
publication with the next revision; never replace a release or rerun an already
published tag.

## Reporting security issues

Do not open a public issue for a security vulnerability. See
[SECURITY.md](SECURITY.md).

## Code of conduct

Be straightforward and civil. Critique the change, not the person. See
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for the standard and reporting process.
General open-source questions can go to <opensource@labstack.com>.

The Kong tree in `internal/cli` owns command parsing and confirmation. Shared
kill, cleanup, acknowledgment and jump execution lives in `internal/actions`,
which must not import `internal/cli`. Chat owns approval transport and permission
waiting/transcript matching; hooks retain quiet fail-open parsing in CLI.
`internal/attention` owns attention policy, notifications and terminal jumps;
its configuration section remains `inbox`. `internal/view` builds presentation
models and `internal/viewer` owns live presentation sources.

Keep command hints and notification callbacks aligned with the
[command reference](docs/usage.md#command-reference). Do not add aliases or
compatibility shims. Group tests by behavior, retaining platform suffixes.
Run `just check` and `just race` with their disposable HOME/data isolation;
web DOM assertions run Node through the Go web tests. Windows vet is part of
`just check` (`GOOS=windows go vet ./...`). Tests must use fixture agent runners.
