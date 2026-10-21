# Driga

**See what your coding agents are doing, and what needs you.**

Driga is a CLI, terminal UI, and local web dashboard for AI coding agent
sessions on macOS and Linux. Windows: experimental (phase 2: always on).
It brings agent activity, project status, an attention inbox, orphaned helpers, and machine health into one place.

Driga is **alpha**. Agent formats and behavior can change; unknown or incomplete
evidence is shown explicitly. Expect changes as the interfaces settle.

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

Download the archive for your platform from
[GitHub Releases](https://github.com/labstack/driga/releases). The release matrix
covers macOS, Linux, and Windows on amd64 and arm64. Windows archives are ZIPs
containing `driga.exe`; real-agent Windows support is still being verified.
Check the archive against its
`SHA256SUMS`, unpack it, and put `driga` on PATH. The
[release guide](docs/release.md#verify-and-install-a-release-archive) gives the
verification and installation steps.

To build from a source checkout, use the Go version in [go.mod](go.mod) or newer:

```sh
CGO_ENABLED=0 go build ./cmd/driga
./driga --help
```

Keep Git on PATH and the installed binary at a stable absolute path before
setting up hooks or a service. CalVer tags use the year as their major number;
Go module major-version rules prevent `go install ...@<tag>` with this module
path. Use an archive or a source checkout.

## Windows (experimental)

Windows 10 1809+ and Windows 11 have native snapshots, foreground collection,
read commands, TUI/web/MCP, OTLP intake and fleet interfaces. Phase 2 adds:

- A per-user Task Scheduler collector at logon: `driga service install`,
  `uninstall`, `start`, `stop`, `status`, `restart`, and `logs [-f]`; no elevation.
- Previewed Claude, Codex and Copilot hook installation with absolute executable paths.
- Native plain toast alerts (no PowerShell); doctor reports availability.
- Terminal jumps/open in Windows Terminal, falling back to a new Command Prompt.

Logs live at `%LOCALAPPDATA%\driga\logs\collector.log`. Keep `driga.exe` at a
stable path before installing. Killing orphan processes, cleanup application,
Claude scratch discovery and socket/file probes remain unavailable. Toast clicks
and focusing an existing terminal are not implemented. Windows remains experimental
until real agent sessions, logon/restart and notifications are verified on hardware.
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
