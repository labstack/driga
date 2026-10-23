# Chat actions — design

Date: 2026-10-23. Phase 2 of Chat. Phase 1 (read-only Chat) is in the
binary today; this phase lets Chat act.

## Intent

Chat should be able to do what the maintainer would do at the keyboard:
- dismiss inbox items, jump to a terminal, kill a stuck agent, clean up stale
  worktrees and refresh summaries;
- run shell commands, edit files and use git in a project, the way Claude Code
  itself does;
- stay easy to understand. The user always knows what Chat is about to do, and
  only the user decides how much it may do alone.

"Neat, easy, simple, powerful without over-engineering."

## Goals

- **One gate.** Every call that changes anything goes through one approval
  path that driga owns:
  - driga's own action tools;
  - Claude's built-in tools: Bash, Edit, Write, WebFetch and the rest.
- **Three trust modes**, switched only by the user:
  - Confirm, the default;
  - Auto;
  - Full auto.
- **Action cards**, the same in the TUI and the web: what will run, on what,
  where; Allow or Deny; then who decided and the result.
- **An action log** kept with the conversation, under the existing capture
  policy and retention.

## Non-goals (this phase)

- Answering a running agent's own permission prompts, and replying into its
  session.
- Dispatching new agent tasks. Chat can still start work through Bash, behind
  a card.
- Actions on paired machines. Chat acts on this machine only.
- Action tools in `driga mcp` for the user's own agents. That server stays
  read-only.
- `driga ask` actions. It has no place to show a card, so it stays read-only.

## Anti-goals (a build that does these has failed)

- The model changing the trust mode, or allowing its own card.
- Text from transcripts, files, tool output or web pages widening what runs.
- Anything running in Confirm mode without the user's click or key.
- A second approval path, for example a tool that runs before asking.
- Typing into a live agent terminal.

## How it works

1. **Tools Chat's Claude gets.**
   - Built-in tools: the default set (`--tools default`).
   - driga's read tools: as today.
   - driga's action tools, new:

     | Tool | Does |
     |---|---|
     | `ack_inbox` | inbox item id, or all |
     | `jump` | session id |
     | `kill` | session or orphan id, optional force |
     | `cleanup` | candidate id |
     | `summarize` | project |

     They call the same code as the CLI commands, keeping every existing
     safety check: kill verification, cleanup re-verification, jump
     resolution.
   - User settings, hooks, plugins and other MCP servers stay off, as today
     (`--setting-sources ""`, `--strict-mcp-config`).
2. **Always allowed, no card.** The driga read tools, and Claude's read-only
   built-ins: Read, Glob, Grep, LS, TodoWrite and similar. These go in
   `--allowedTools`.
3. **Everything else asks driga.** Claude runs with
   `--permission-prompt-tool mcp__driga__approve`. Claude calls that tool
   before any other tool call.
   - The driga MCP child (`driga mcp --chat`) forwards the request to the
     broker over the broker's owner-only socket. It sends the tool name, the
     input and a per-turn token that ties it to the conversation.
   - The broker decides by trust mode:
     - Auto rule matches, or Full auto: allow at once and log it.
     - Otherwise: emit a pending action card and wait for the user.
   - The broker returns Claude's permission answer:
     - `{"behavior":"allow","updatedInput":…}`
     - or `{"behavior":"deny","message":…}`
4. **Decisions only come from clients.** The TUI key or the web button calls
   the broker's `Decide(conversation, action, allow)`. The MCP child can only
   ask. It has no way to decide.
5. **Stop and timeout.**
   - Stop denies every pending card and then stops the turn, as Stop does
     today.
   - A card nobody answers is denied after 10 minutes, with status
     `expired`.
6. **Remote targets.** A target on another machine is refused with a
   readable reason.
7. **No service installed.** The client that runs the broker in-process also
   serves it on a private socket of its own, so the MCP child can still reach
   it.
8. **Missing support.** If the installed Claude CLI lacks the permission
   bridge, Chat stays read-only and says why.
   - `--permission-prompt-tool` is hidden from `claude --help` in 2.1.309, so
     the probe requires the visible `permission-prompts` flag as its marker.
   - Chat passes `--permission-prompts host` explicitly.

**Verified by hand** against Claude Code 2.1.309, in `-p` mode with
`--permission-prompt-tool` pointing at a probe MCP server:
- The tool received `{tool_name, input, tool_use_id}` before every
  file-changing Bash command and before Write.
- `{"behavior":"deny","message":…}` blocked the call, and Claude reported the
  message back to the user.
- `{"behavior":"allow","updatedInput":…}` ran the call.
- Commands Claude itself classifies as read-only (`echo`, `cat`, Read) ran
  without calling the tool. This is Claude Code's normal read-only behaviour,
  the same as in its own terminal. driga accepts it: reads are allowed in
  every mode anyway.

## Trust modes

| Mode | Runs without asking | Asks |
|---|---|---|
| **Confirm** (default) | Reads only | Every other call |
| **Auto** | Reads, plus `ack_inbox`, `jump`, `summarize`, and Edit or Write inside the conversation's working directory | Bash, `kill`, `cleanup`, WebFetch, writes outside the working directory, anything new |
| **Full auto** | Everything | Nothing; header shows it in red |

- The mode belongs to one conversation and starts as Confirm.
- Full auto runs for 30 minutes, then drops back to Auto by itself.
  - The header shows the time left.
  - Switching it on again restarts the 30 minutes.
  - It also ends when the user switches it off, the conversation is closed,
    or the broker restarts. It never carries over.
- Why the limit: Chat reads other sessions' transcripts, files and web pages.
  Text in any of them can try to steer the model (prompt injection). In
  Confirm and Auto, a shell command, kill or cleanup still stops at a card.
  In Full auto nothing stops it, and it runs with the user's full account
  access. A short window keeps that exposure deliberate and brief.
- Switching to Full auto asks once, in the client, to confirm. The
  confirmation says that commands will run without cards for 30 minutes.
- Unknown tools always ask in Auto, so new tools never run silently.

## Action cards

A card is a new conversation event, `action`, and a stored chat message with
role `action`. It holds:
- id;
- tool;
- a one-line title, for example "Kill claude #7 · driga main · 4 processes";
- the exact input: the command, the file path and diff size, or the target
  ids;
- status: `pending`, `allowed`, `denied`, `expired`, then `done` or `failed`;
- who decided: `you`, `auto` or `full auto`;
- a short result.

Titles for driga tools come from driga's own data. Built-in tools show their
input verbatim: the Bash command, the file path.

**TUI.**
- A pending card shows in the conversation, and the status line says "action
  waiting".
- In navigate mode, `y` allows and `n` denies the oldest pending card. While
  typing, use `alt+y` and `alt+n`.
- `m` cycles the trust mode, in navigate mode.

**Web.**
- Allow and Deny buttons sit on the card.
- The existing trust-mode buttons become live.
- Decisions are authenticated POSTs with the existing Origin and CSRF checks.

**Both.** The header shows the mode, and Full auto shows in red.

## Left to the builder

- Package layout. A small `internal/chat/actions.go` is enough; no new
  package unless it earns its place.
- How each action tool reuses the CLI code paths.
- Card wording within the existing theme tokens.
- Test doubles for the permission bridge.
- The real-CLI smoke test is done once, by hand, outside the test suite.
