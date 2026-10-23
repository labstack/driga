# Answer agent prompts from driga — design

Date: 2026-10-23. Phase 3 of Chat. Phase 2 (Chat actions) is in place.

## Intent

When a Claude Code session asks "Do you want to proceed?", the user should be
able to answer it from driga instead of finding that terminal:
- from the inbox, in the TUI or the web;
- from Chat.

The terminal prompt keeps working exactly as before. Whichever answer comes
first wins.

"Neat, easy, simple, powerful without over-engineering."

## Verified behaviour (Claude Code 2.1.309, interactive, by hand)

The probe was a `PermissionRequest` command hook that waits 25 seconds and
then denies:
- The terminal shows its normal permission dialog while the hook is still
  running.
- When the hook answers first, its decision applies. The terminal dialog
  closes, and Claude reports the deny `message`.
- When the user answers in the terminal first, that answer applies. Claude
  ignores the hook's later output, but does not stop the hook process.
- The session transcript gets the `tool_use` when the prompt opens, and the
  matching `tool_result` right after the terminal answer. That is how the
  hook can tell it was answered elsewhere.

From the docs:
- Default command hook timeout is 600 s.
- Deny rules still apply after a hook allows.
- `updatedPermissions` exists; driga never uses it.

Codex's `PermissionRequest` hook runs before Codex shows its own prompt. A
waiting hook would hide that prompt, so Codex is out of scope here (see
Later).

## Goals

- Pending Claude Code permission prompts appear in driga with:
  - the session;
  - the tool;
  - the exact input: the command, or the file and change size.
- **Allow** or **Deny** from:
  - the TUI inbox;
  - the web inbox;
  - Chat.
- The answer reaches the waiting session within a second.
- A prompt answered in the terminal leaves driga within a second or two.

## Non-goals

- Codex, Copilot and Gemini prompts.
- Replying to a session with a new message.
- "Always allow" rules. driga never writes `updatedPermissions`.
- Prompts on paired machines. Prompts are answered on the machine itself.

## Anti-goals

- The terminal prompt blocked, delayed or changed in any way.
- A driga failure that denies a request. Every failure leaves the decision to
  the terminal.
- Prompt inputs written to the database, the log, the fleet, or to Chat
  beyond its capture policy.
- Chat answering prompts outside its own trust-mode gate.

## How it works

1. **Hook.** `driga hooks install` adds a Claude `PermissionRequest` hook,
   `driga hook claude PermissionRequest`, with `timeout: 660`.
   - It is shown in the diff and needs approval, like the existing hooks.
   - Repair and uninstall cover it too.
2. **Hook process.**
   - It reads the request: `session_id`, `cwd`, `transcript_path`,
     `tool_name`, `tool_input`.
   - It connects to the collector's broker socket within about 200 ms. If no
     collector is running, it exits 0 with no output, and the terminal decides.
   - It registers the prompt and waits up to 10 minutes. Meanwhile it watches
     the transcript for the `tool_result` of the session's last `tool_use`
     with the same tool name and input.
     - If that result appears, the prompt is retired as "answered in
       terminal" and the hook exits with no output.
     - On a driga decision, it prints the allow or deny `decision` JSON, with
       the message "Denied from driga".
     - On timeout, session end or any error, it exits with no output.
3. **Broker.** It keeps pending prompts in memory only. They are never stored,
   and they disappear on restart.
   - Hook side, over the owner-only socket: register and wait, then retire.
   - Client side: `Prompts(ctx)` and `AnswerPrompt(ctx, id, allow)`.
   - A prompt is answered at most once. Late answers get "prompt no longer
     waiting", including on the Chat card.
     Removal and answering share the broker lock; a committed answer is delivered
     even if the hook cancels at the same time.
   - A prompt is also retired when its session ends.
4. **Inbox.** Each pending prompt is an inbox row on top of the existing
   "needs you" item for that session, showing the tool and input.
   - TUI keys: `a` allow, `d` deny. Check for clashes with existing inbox
     keys and pick free keys if needed.
   - Web: Allow and Deny buttons, as authenticated POSTs with the existing
     Origin and CSRF checks.
5. **Chat.**
   - Only action-enabled Chat turns register `list_prompts`. It shows tool names
     and inputs only when the capture policy is `snippets`; otherwise the tool
     name only.
   - Chat reads inside the working directory run; reads elsewhere ask in
     Confirm and Auto, and Full auto runs them. Read, Glob, Grep, LS and
     NotebookRead stay out of `--allowedTools`; TodoWrite stays allowed.
   - An action tool, `answer_prompt(id, allow)`, goes through the phase 2
     gate:
     - Confirm: shows a card.
     - Auto: always asks, like Bash.
     - Full auto: runs.

## Left to the builder

- Socket endpoints and payload shapes.
- Transcript matching details (tail-read, bounded, tolerant of partial lines).
- Exact inbox row layout and key choice.
- Tests with a fake transcript writer and a fake hook client. No real agent
  CLIs in tests.

## Later

- **Codex:** answer from driga only while the user has chosen to route Codex
  approvals to driga (for example, while they are away), because a waiting
  hook hides Codex's own prompt.
- **Reply to a session:** a new turn through the agent's resume interface,
  only when no live process owns the session.
