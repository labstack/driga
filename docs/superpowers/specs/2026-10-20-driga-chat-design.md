# Chat — design

Date: 2026-10-20. Status: draft for the maintainer's approval. Look: mockup https://claude.ai/artifact/AZVVyp87LAzK4gCwirVpNK. Architecture reviewed with Codex. Items marked *(author's call)* were not decided by the maintainer.

## Intent

The maintainer wants one place, inside driga's web console and TUI, to talk to: to understand what is going on across every agent session, to act on it, and to start new work, without switching to terminals. It must feel professional and cool, and it must be able to do anything the maintainer could do. "Chat" is the name; it sits beside Inbox and Flow.

## Goals

- **Understand.** Answer from driga's data: who needs you and why, what a session is doing and why driga labels it so, catch-up digests, conflicts, orphans, machine and fleet health, and "what does this screen mean". It knows what the user is looking at (page, selected session) as removable context.
- **Act.** Approve or deny an agent's pending request, reply to a session as a new turn, acknowledge, jump, clean up, kill, edit files, run shell commands, use git.
- **Dispatch.** Start a task with a chosen agent on this machine or a paired one; list, follow and cancel tasks.
- **Watch.** Standing requests ("tell me when #412 finishes", "alert on memory pressure"), run by driga, delivered to the Chat and the inbox.
- **Any agent.** The user picks Claude, Codex or Copilot per conversation. Each runs through its own CLI and the user's own login. No API keys in driga.

## Non-goals

- Replacing the inbox, alerts, Flow map or views: Chat explains and drives them; they stay the way to see.
- A hosted service or phone app. Chat runs where driga runs.

## Anti-goals (a build that does these has failed)

- Driga sending data anywhere new. The only destination is the chosen agent CLI, which already talks to its own vendor.
- Chat changing its own trust mode or resolving its own confirmation cards. Only the user's click or key does that.
- Text from a transcript, file, tool output or web page silently widening what Chat may do.
- Typing into a live agent terminal (no tmux `send-keys`, no PTY injection). Replies go through the agent's own resume or control interface.

This revises the v1.8 control design's anti-goal against auto-approval: approval without a per-request click is now allowed, but only under a trust mode the user switched on.

## Trust modes

| Mode | Behaviour |
|---|---|
| **Confirm** (default) | Every action is an action card: exact target, command or change, and where; runs on the user's click or key. |
| **Auto** | Action kinds the user ticked (for example: approve test commands, acknowledge, orphan cleanup) run by themselves; everything else asks. |
| **Full auto** | Everything runs without asking. Shown in red in the header with what it covers. *(author's call)* It is scoped to a machine (and optionally a project) and lasts until the user switches it off or a chosen time limit ends (default 2 hours). |

In every mode: an action log records the request, who or what decided it, what ran and the result; Stop (button, `esc`) cancels pending cards, stops the Chat's agent and terminates processes Chat started, after a grace period. Undo is offered only where a real reverse exists.

## How it works

- **Broker.** A long-lived broker inside driga (the service process, so the web and TUI share conversations) runs the chosen agent as a child process in its streaming mode and keeps the vendor session ID to continue the conversation:
  - Claude: `claude -p --output-format stream-json --verbose --include-partial-messages`, `--resume <id>`, `--mcp-config <file> --strict-mcp-config`, `--permission-prompt-tool mcp__driga__permission`.
  - Codex: `codex app-server` (its bidirectional protocol exposes command and file approval requests).
  - Copilot: `copilot --acp --stdio` once ACP is out of preview; until then Copilot chat is read-only *(author's call)*.
  Flags are from current docs and must be probed against installed versions; an adapter that lacks a needed control fails closed (read-only) and says why.
- **Tools.** The agent gets driga's MCP server: today's read tools plus new action tools (`approve`, `deny`, `reply`, `ack`, `jump`, `cleanup`, `kill`, `dispatch`, `get_task`, `list_tasks`, `cancel_task`, `subscribe`, `unsubscribe`, `list_actions`).
- **One gate.** Every effect goes through a new `internal/control` package used by MCP, web, TUI and fleet alike. It checks the exact request (canonical arguments, machine, project, session), freshness, idempotency and the trust mode immediately before running it.
- **The agent's own shell and edits are allowed** (the maintainer wants Chat to do anything), but Chat's agent runs with **its own driga-managed settings**, not the user's: no inherited allow rules, so every native shell, edit or git action asks, and the question becomes a driga action card (Claude's `--permission-prompt-tool`, Codex app-server approvals). The trust mode then decides: Confirm shows the card, Auto approves the ticked kinds, Full auto approves everything. Every tool call is also read from the agent's stream into the action log. This closes the gap where an action already on the user's allow list would run without a card.
- **Untrusted content.** Transcripts, tool output and files are passed to the agent as quoted evidence with their source. The trust mode, not the model, decides what runs.
- **Privacy.** The capture policy (`titles`/`snippets`) is applied before anything goes to the agent, to the log or to the fleet. Vendor-side conversation history is the vendor's; driga keeps its own copy under the same retention as other data.

## Dispatch

A dispatched task is always a new, separate agent session in its own git worktree and branch, registered with a parent link to the Chat so it appears on the Flow map as "dispatched by Chat". The dispatch card shows task, project, worktree, branch, machine (with free agent slots), agent and access before it starts. Concurrency is bounded per machine. On a paired machine, the target machine's own driga authorizes the task; being paired never grants the right to run work. *(author's call)* Merging is never part of a dispatch; it is a separate action the user can ask Chat for.

## What changes elsewhere

| Feature | Decision |
|---|---|
| `driga ask` | Merged: becomes a one-shot client of the broker. |
| Opt-in `claude -p` summaries | Merged into the broker's agent adapter; still opt-in and cached. |
| Planned approve, reply and dispatch screens (v1.8 steps 2–5) | Merged: one shared action card used by Chat and by buttons on inbox cards. |
| Web command bar | Kept for instant navigation; non-command text opens Chat with that text. |
| Help and legends | Kept, trimmed. |
| Inbox, alerts, jump, Flow, views, MCP, CLI `--json`, hooks, collector, fleet | Kept; Chat builds on them. |

## Interface

Web: Chat docks on the right of every console page (toggle `c`) and has its own tab `8:chat`. Header: agent picker, trust mode with a one-line description, Stop. Conversation: user messages, agent text streaming in, a collapsed list of tools used, session chips that select the session on the page, and action cards (Approve, Dispatch, Shell, Reply, Kill...) that resolve in place with who decided and when. Composer: context chips (current page, selected session, machine), multiline input, `⌘↵` send, `/` commands. Theme tokens from `internal/view/theme.go`; motion bound to events, off under reduced motion.

TUI: tab `8 Chat`, and `c` opens it from any tab with the current selection as context. Box-drawing action cards with keys (`a` approve, `d` deny, `s` start, `e` edit, `x` cancel), `m` trust mode, `L` action log, `esc` stop. Same data and behaviour as the web.

Security: the existing loopback binding, Host check, token and CSP stay; action requests become authenticated POSTs with Origin checks and CSRF tokens; streamed output is escaped; SSE gains ordered replay for the conversation stream.

## Phases (each ships on its own)

1. **Chat that understands** — broker with Claude, read tools, web panel then TUI, streaming, resume, context chips, action log skeleton. Retires: login, streaming and resume behaviour of the installed CLI.
2. **Actions** — `internal/control`, action cards, trust modes, Stop, secure POSTs; acknowledge, jump, cleanup, kill first; agent tool calls logged. Retires: bypass, stale approval and double execution.
3. **Approve and reply** — Claude permission bridge and Codex app-server; reply as a new turn. Retires: approval ownership, timeouts, two writers on one session.
4. **Dispatch here** — worktrees, owned processes, Flow parent links, concurrency limits, Watch subscriptions. Retires: lifecycle and recovery of started sessions.
5. **Dispatch to paired machines** — fleet task protocol with target-side authorization and receipts.

Copilot moves from read-only to full Chat when ACP leaves preview.

## Left to the builder

Package layout under `internal/chat` and `internal/control`, the conversation storage schema (new migration), adapter probing details, exact card layouts within the mockup's look, and how the Auto kinds list is presented.

## Decisions from the builder check

Answered by the maintainer's "Chat can do anything":
- **Approve and reply cover any session**, not only Chat's own. For terminal Claude sessions this needs a permission hook that holds the request in driga until decided, with the agent's normal prompt as the fallback on timeout (v1.8 step 2's mechanism). It edits `~/.claude` settings, so install shows the diff and asks, like other hook installs. Phase 3.
- **Kill may target any process the user's account owns**, including agent sessions; it is an action kind like any other and runs under Full auto. The v1.8 rule "never signal a process driga did not start" is revised for Chat actions.
- **Committing and pushing are allowed** as actions (through git in the agent's shell or a driga action). Dispatched tasks commit to their own branch. Merging stays a separate, explicit request.
- **Full auto covers every action kind.** No kind is always-confirm.

Author's calls:
- **Broker location.** The broker runs in the collector service and listens on a Unix socket in the data dir (owner-only permissions); `driga web`, the TUI and `driga ask` are clients of it. With no service installed, the client that opens Chat runs the broker in-process and says that conversations end with it.
- **Chat's agent runs with driga-managed settings** (its own permissions file, no inherited allow rules, user hooks and plugins off), and uses the user's own login. Its working directory is a driga-managed Chat directory in the data dir by default, and the selected project when a project or session context chip is present. Full auto's project scope limits which project driga's actions may target and which directory the agent starts in; it does not sandbox the agent's shell.
- **Capture policy** applies to everything driga's tools return, to the action log and to the fleet. The agent's own shell can read whatever the user's account can read; the header states this when native permissions are broad.
- **Remote machines.** Phases 2–4 act on this machine only; remote acknowledgement keeps working as today. Remote actions arrive with phase 5's task and control protocol.
