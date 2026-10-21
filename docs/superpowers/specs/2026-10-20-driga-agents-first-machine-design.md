# Agents-first machine view — design

Date: 2026-10-20. Status: approved in chat by the maintainer ("Ok"). Revises Part 1 and Part 3 of `2026-10-20-driga-machine-health-design.md`. Items marked *(author's call)* were not decided by the maintainer.

## Why

The maintainer asked that resource monitoring be "with respect to agents". The current Machine view reads like a general system monitor: whole-machine vitals, MCP rows, then top 10 apps by memory and CPU across every app (ChatGPT, browsers, backup tools), with agent memory by role last. It never says how much of the machine the agents use. Two independent reviews (Fable and Codex) agreed: machine health belongs in driga, because agent-spawned helpers and MCP servers are a real cause of pressure, but the view must lead with agent-caused cost and treat the rest of the machine as context. Replacing `top` or Activity Monitor is not driga's job.

## Goals

1. **Agents' share first.** One summary line: agents' memory and CPU against the machine's, process count, MCP server count, pressure, swap, free disk. Example: `agents 14.2 of 34 GB (42%) · 9% CPU · 2,013 processes · 1,980 MCP servers · pressure critical · swap 81/83 GB · disk 25 GB free`.
2. **Agent cost by session.** Each live session's own processes plus everything attributed to it (helpers, MCP servers, dev servers), as one total, largest first.
3. **Agent processes without a session.** Processes driga identifies as agent processes (by signature or by an agent host ancestor) but cannot tie to a session, as their own group. "Unattributed" never means "not an agent".
4. **Everything else collapsed.** All other apps in one "Other apps · 19.8 GB" row, collapsed by default and expandable to the existing per-app list, so Chat can still answer "why is my Mac slow?".

## Where agents run (maintainer decisions, 2026-10-20)

- **Observe agents wherever they were started.** People run the same agents from a terminal, from editor extensions (Claude Code and Codex in VS Code, Copilot agent mode, JetBrains, Xcode), from desktop apps (Claude.app, ChatGPT) and from scripts using an SDK. The maintainer: "You don't know how people use it… we need to be clever in design." Rules come from what a process does, not from a fixed list of apps.
- **Dispatch uses the default CLI programs** (`codex`, `claude`, `copilot`). Driga never has to drive an editor to start work.
- Each session carries where it was started (terminal, editor name, desktop app, SDK), shown in lists, the Flow map and alerts.
- Cost follows the session: an agent with its own process counts its whole process tree, even under an editor parent; an agent that runs inside an editor's extension host counts only what it starts (terminals, MCP servers, dev servers), and the host is labelled "shared with editor". The editor's own windows, language servers and plain extensions stay under "Other apps".
- Agent evidence, strongest first: signature data naming an agent; a process that hosts MCP servers; a process that talks to an AI model service and starts tools; a user mark in config. The headline separates proven from likely ("agents 14 GB proven + 3 GB likely").
- Fixtures follow the researched process shapes below. Where a shape is unverified (Xcode Codex argv, Cursor and Windsurf terminal parents, Agent Host session storage), the rule is written so it does not depend on it, and the gap is listed.

## Editor agents: what research found and what this work covers

Research (vendor docs, source, issue reports; not a live capture, which the sandbox prevented) shows two shapes:

- **The editor starts a known agent program.** The Claude Code extension spawns its bundled `claude`; the Codex extension spawns `codex app-server`; VS Code's Agent Host and Copilot background sessions run the Copilot runtime as a child process; JetBrains runs agents over ACP (`junie --acp`, Claude, Codex); Xcode 26.3+ runs its own bundled Claude and Codex. Driga already sees these processes. Its MCP servers are the agent program's children, as in the terminal case.
- **The agent loop runs inside the editor's extension host.** VS Code's local Copilot harness, Cline, Roo Code, Continue, Cursor's agent. MCP servers are children of the extension host; agent terminals are children of the editor's pty host.

**In scope (the smaller, structural part):**
1. **Launch surface from the parent chain.** A known agent process whose ancestors reach an editor or desktop app bundle is labelled with that app ("VS Code", "Xcode", "JetBrains"), using the existing `.app` bundle rule. No product list beyond the existing signatures.
2. **Extension hosts that parent MCP servers count as agent hosts, "likely" and "shared with editor".** Their MCP servers count as likely agent cost; the extension host's own memory is shown but not added to agents' totals, since it cannot be split between extensions. Orphaned MCP servers (parent gone) stay in the existing pile-up and growth markers.
3. **Proven versus likely in the headline** as above.

**Deferred (each is a separate adapter, added only when someone needs it):** reading VS Code `chatSessions`, Cursor `state.vscdb` and Windsurf/Cursor `hooks.json` for session state; Xcode's separate `CLAUDE_CONFIG_DIR` (its Claude transcripts are not in `~/.claude`); attributing pty-host terminals to a session by cwd and time; live Copilot-in-VS Code testing (needs a signed-in account).

## Non-goals and anti-goals

- No per-core, GPU, network or temperature readings.
- No advice to close non-agent apps; driga can show them, never recommend acting on them.
- No generic tool-cache cleanup: the `cache` kind (Go, npm, Homebrew, simulators) is dropped from the machine-health plan. `deps`, `agent-versions` and `archived` stay in that plan but are not part of this work *(author's call)*; alert text here points only to sessions and helper groups.
- No thresholds in absolute GB or process counts tuned to one machine.
- No automatic killing or deleting.

## Rules

- **Totals from the full sample.** Agents' share is computed from every sampled process, each counted once, never from the top-10 lists. A process belongs to exactly one bucket, checked in this order: attributed to a session; agent process without a session; other app.
- **Agent process.** See "Where agents run": evidence-based, proven versus likely. Role-only signature matches that name no agent (`desktop-app`, `generic-mcp-launcher`, `driga-internal`) are not agent evidence on their own.
- **Attribution.** Unchanged proof rules from the machine-health design. The two proof kinds are shown differently: "started by" (parent chain) versus "in project, started later" (cwd plus start time), since the second is weaker evidence.
- **Memory measure labelled.** Totals say which measure was used (physical footprint or RSS) and keep the existing "may exceed RAM" note.
- **Wording.** "Leak" becomes "observed growth" (additions with no exits in the observed window), and its alert kind keeps that wording. Rows under an agent host that are not proven MCP servers are labelled "helper processes", not "MCP servers"; a process is called an MCP server only when its command or signature says so *(author's call: the exact proof rule is left to the builder, see below)*.

## Alerts

- Keep `low_disk` and `memory_pressure` as they are: OS-reported signals that threaten agent work whatever their cause.
- Their text leads with the agent-caused part and what driga can do about it ("agents hold 14 GB, 1,980 MCP servers; 3 idle sessions hold 4 GB"). When agents are a small share, say so plainly ("agents hold 1.2 GB of 34 GB") instead of implying they are the cause.
- No new growth alert in this work *(author's call)*: today's trends have no windows, so "observed growth" stays a marker in the view until a windowed rule is designed and validated.

## Surfaces

- **TUI Machine tab and web Machine page**, in this order: summary line; sessions by cost (expandable to processes with their proof); agent processes without a session (grouped by command, with pile-up and observed-growth markers from the existing roll-ups); "Other apps" (collapsed); machine vitals detail.
- **MCP `get_machine` and `driga machine --json`** gain the same buckets and the agents' share totals, so Chat answers from them.
- The old top-10 all-apps sections disappear as separate sections; their data lives under "Other apps" and under each bucket.
- **JSON changes are additive only** *(author's call)*: schema stays 2, `top_memory`, `top_cpu` and the `"leaking"` state value stay as they are (only the displayed wording changes), and the buckets and agents' share are new fields. Older and newer driga versions share fleets, and Chat prompts read this schema.

## Constraints

- Not curve-fitted (design §2a). Buckets come from mechanisms (signatures, parent chains, proof rules), not from names seen on the maintainer's Mac. Validate on held-out fixtures that are not the maintainer's process list.
- The collector stays cheap: no new process scans, and expensive per-process reads (footprint) stay limited to what the existing sample already reads. The heap-budget and cost tests must still pass, and the live service must stay within the original design's CPU target.

## Testing

Fixtures where: a helper is attributed by parent chain and another only by cwd; an agent process has no session; a non-agent app spawns a `node` process (must stay in "Other apps"); totals equal the sum of buckets with nothing counted twice; agents are a small share (alert text says so). Golden output for the TUI at 80 and 140 columns and for the web page.

## Left to the builder

The exact rule for "proven MCP server" versus "helper process"; layout and colours of the summary line; how many sessions show before "more"; whether the vitals detail sits above or below "Other apps".
