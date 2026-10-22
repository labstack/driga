<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="internal/web/static/brand/lockup-white.svg">
    <img src="internal/web/static/brand/lockup.svg" alt="Driga" width="300" height="107">
  </picture>
</h1>

**See what your coding agents are doing, and what needs you.**

Driga is a CLI, terminal UI, and local web dashboard for AI coding agent
sessions on macOS and Linux. Windows: experimental, including orphan kill and
worktree cleanup.
It brings agent activity, project status, an attention inbox, orphaned helpers, and machine health into one place.

Driga is **alpha**. Agent formats and behavior can change; unknown or incomplete
evidence is shown explicitly. Expect changes as the interfaces settle.
Pre-release databases must be reset; `driga doctor` prints the reset command.

[Usage](docs/usage.md) · [Machine health](docs/machine-health.md) · [Releases](https://github.com/labstack/driga/releases)

## Why Driga

| Concern | What Driga shows |
| --- | --- |
| Agent activity | Claude Code, Codex, GitHub Copilot CLI, Gemini CLI, and process-level Antigravity (`agy`) sessions, grouped into families. |
| What needs you | An inbox for attention requests and completions, with explicit acknowledgment. |
| Project status | Local Git branches, worktrees, dirty state, and file overlaps between agents. |
| Leftovers | Orphan helper process candidates and local disk cleanup candidates, with evidence and confirmation before removal. |
| Machine health | Memory, CPU, pressure, disk, and agent process attribution. |
| Multiple machines | Optional fleet views over pinned mutual TLS, with explicit pairing and sharing controls. |
| Other interfaces | Versioned JSON, read-only MCP tools, a loopback web dashboard, and opt-in summaries and Chat through Claude. |

## Install

On macOS, install the signed and notarized release with Homebrew:

```sh
brew install --cask labstack/tap/driga
```

On Windows (experimental), use Scoop:

```powershell
scoop bucket add labstack https://github.com/labstack/scoop-bucket
scoop install labstack/driga
```

Debian `.deb` and RPM `.rpm` packages, plus archives for macOS, Linux, and
Windows on amd64 and arm64, are available from
[GitHub Releases](https://github.com/labstack/driga/releases). Windows archives
contain `driga.exe`; macOS and Linux use tar.gz archives. Verify downloads
against `driga_<version>_checksums.txt` before installing. The
[release guide](docs/release.md#verify-and-install-a-release-archive) covers
checksums, packages, and archive installation.

To build from a source checkout, use the Go version in [go.mod](go.mod) or newer:

```sh
CGO_ENABLED=0 go build ./cmd/driga
./driga version
```

Keep Git on PATH and the installed binary at a stable absolute path before
setting up hooks or a service. CalVer tags use the year as their major number;
Go module major-version rules prevent `go install ...@<tag>` with this module
path. Use a package, archive, or source checkout.

## Windows (experimental)

Windows 10 1809+ and Windows 11 have native snapshots, foreground collection,
read commands, TUI/web/MCP, OTLP intake and fleet interfaces, plus:

- A per-user Task Scheduler collector at logon: `driga service install`,
  `uninstall`, `start`, `stop`, `status`, `restart`, and `logs [-f]`; no elevation.
- Previewed Claude, Codex and Copilot hook installation with absolute executable paths.
- Native plain toast alerts (no PowerShell); doctor reports availability.
- Terminal jumps/open in Windows Terminal, falling back to a new Command Prompt.
- Confirmed killing of driga-proven orphan trees, with pinned process start
  times, socket checks, a graceful attempt and optional forced termination.
- Confirmed cleanup of verified linked worktrees into the Recycle Bin, with
  the same revalidation and audit logging as Unix cleanup.

Logs live at `%LOCALAPPDATA%\driga\logs\collector.log`. Keep `driga.exe` at a
stable path before installing. Phase 3 is feature-complete pending real-hardware
verification; Windows remains **experimental**. TCP/UDP probes cover IPv4 and IPv6;
open-file evidence uses bounded Restart Manager queries. Claude scratch cleanup
stays refused because its Windows layout is unverified in repository fixtures. Toasts are plain and
terminal jumps open a new window. Real agent sessions, logon/restart, notifications,
orphan shutdown and Recycle Bin restore still need verification on Windows hardware.
A separate Windows end-to-end CI job builds the CLI and exercises attribution,
collection, Task Scheduler, web assets, hooks, orphan kill and Recycle Bin cleanup
on GitHub-hosted `windows-2025` runners.
See [Windows setup details](docs/usage.md#windows-experimental).

## Quick start

```sh
driga
driga agents
driga projects
driga top
```

Without a collector, read commands take a disposable snapshot and show
`snapshot · no history`. That collection writes no persistent database. One
agent in one repository works without configuring roots or a service.

For guided setup, run `driga init`. Repository roots, text capture, hooks, and
service installation are optional steps. Session working directories also
contribute to repository discovery; there are no default scanning roots.

For ongoing history and inbox events:

```sh
driga service install
driga doctor
driga inbox --since yesterday
```

Service installation starts a per-user collector. The first import marks older
events as already seen. Reading an inbox item never acknowledges it; use
`driga inbox ack <item-id>` or the Inbox controls in the TUI.

Use `driga web` for the local browser dashboard, `driga top --project .` to focus
on the current project, and `driga <command> --help` for command-specific flags.

## Privacy and what it reads

Driga reads local agent session metadata and logs, the process table, machine
health, and local Git state. Claude and Codex histories, Gemini chat files,
and optional hook events provide session evidence. Copilot uses hooks and
process observations; Antigravity uses process observations only. The Codex
catalog is opened read-only. Local Git commands never fetch.

The default `capture_text = "titles"` stores generated titles, file paths, and
session/process/Git/machine metadata. Prompt and handoff snippets require
`capture_text = "snippets"`; snippets are scrubbed and capped at 200 characters.
Full transcripts are not stored. Paths and titles can identify you, and secret
scrubbing cannot guarantee privacy.

Collection stays local unless you configure fleet peers. Fleet sharing defaults
to metadata and titles; the serving peer controls text sharing. Optional OTLP
intake listens on loopback. The web dashboard listens on loopback and requires
a local access token.

Optional summaries and Chat run Claude through your installed CLI and its
existing vendor connection. Explicit questions and permitted context can leave
the machine through that connection. Agent configuration changes only through
explicit setup and hook commands. See [privacy and configuration](docs/usage.md#privacy-and-configuration)
for paths, capture settings, and the fleet sharing table.

## Documentation

- [Brand assets](docs/brand.html): Murmur logos, icons, and usage guidance.
- [Usage and configuration](docs/usage.md): commands, hooks, service, fleet,
  cleanup, Chat, web dashboard, MCP, and OTLP.
- [Machine health](docs/machine-health.md): attribution, measurements, trends,
  and early warnings.
- [Design](docs/specs/2026-10-18-driga-cli-tui-design.md): architecture and the
  reasoning behind collection, state, and privacy boundaries.
- [Release guide](docs/release.md): CalVer, packaging, and archive verification.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and checks. Contributions require
accepting the [Contributor License Agreement](CLA.md). Report vulnerabilities
through [SECURITY.md](SECURITY.md). Participation follows the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

[Apache License 2.0](LICENSE) · Copyright 2026 LabStack LLC. The Driga name is
covered by the [trademark policy](TRADEMARK.md).
