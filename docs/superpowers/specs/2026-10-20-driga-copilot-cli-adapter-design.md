# Copilot CLI adapter — design

Date: 2026-10-20. Status: approved for implementation by the maintainer ("go ahead"); marked calls are the author's.

## Why

GitHub Copilot CLI has the largest installed base of any terminal agent driga does not cover yet, and it ships an official hook system whose events map onto driga's states. Copilot agent mode in VS Code uses the same hook format, so the same work later covers those sessions too. The cloud coding agent runs on GitHub and is out of scope.

## Goals

- Copilot CLI sessions appear in `agents`, the TUI, the web console, MCP and the needs-you inbox with the same states as Claude and Codex: working, waiting (needs you), idle, finished, plus the mixer's stalled and unknown.
- Permission prompts and elicitation dialogs become needs-you items with a jump target (tmux pane by TTY) and a resume command.
- Hook capture stays report-only: it can never block, deny, modify or slow a Copilot session.

## Non-goals (v1)

- Parsing Copilot's on-disk session files (`~/.copilot/session-state/<id>/events.jsonl`). The format is undocumented; v1 relies on hooks and processes. Revisit after a real capture.
- OpenTelemetry intake from Copilot. Only vendor blogs document `COPILOT_OTEL_*`; revisit when GitHub documents it.
- Approve or deny from driga (that is v1.8 step 2 for every agent).
- VS Code Copilot sessions (same hooks, but no terminal or process to link; later).

## Facts this design relies on

From the official reference (docs.github.com/en/copilot/reference/copilot-cli-reference/cli-hooks-reference), checked 2026-10-20 against Copilot CLI 1.0.97 being installed on the maintainer Mac:

- User hooks load from `*.json` in `~/.copilot/hooks/`, or `$COPILOT_HOME/hooks/` when `COPILOT_HOME` is set. All sources are combined; changes load when the CLI starts.
- Config: `{"version": 1, "hooks": {"<event>": [{"type": "command", "exec": "...", "args": [...], "timeoutSec": N}]}}`. `exec`/`args` run without a shell.
- Every payload has `sessionId`, `timestamp` (Unix ms) and `cwd`. Payloads are JSON; the docs imply stdin (`INPUT=$(cat)`) but do not state it. The implementation reads stdin and must be verified on a real run before release.
- Events used: `sessionStart` (`source`: startup, resume, new), `userPromptSubmitted` (`prompt`), `postToolUse` (`toolName`, `toolArgs`, `toolResult`), `postToolUseFailure`, `notification` (`message`, `title?`, `notification_type`), `agentStop` (`transcriptPath`, `stopReason`), `errorOccurred` (`error: {message, name, stack?}`, `errorContext`, `recoverable`), `subagentStart`, `subagentStop`, `sessionEnd` (`reason`). `errorOccurred` output is not processed by Copilot; driga still prints nothing and exits 0.
- `notification_type` values: `shell_completed`, `shell_detached_completed`, `agent_completed`, `agent_idle`, `permission_prompt`, `elicitation_dialog`.
- `permissionRequest` fires before rule checks and auto-approval, so it does not mean a person is being asked. It also accepts decisions. driga does not hook it.
- Timeouts are fail-open. `preToolUse` and `permissionRequest` fail closed on non-zero exit; `agentStop` can force another turn with `decision: "block"`. driga hooks neither `preToolUse` nor `permissionRequest`, prints nothing to stdout, and always exits 0.

## Design

### Hook install

- `driga hooks install copilot` writes one file it owns: `<copilot home>/hooks/driga.json`, where copilot home is `$COPILOT_HOME` or `~/.copilot`. No merging into user files. Same diff-and-confirm flow as the other installers; `driga hooks uninstall copilot` deletes only that file; `driga doctor` reports installed, missing or stale (wrong binary path).
- Each entry: `{"type": "command", "exec": "<stable driga path>", "args": ["hook", "copilot", "<event>"], "timeoutSec": 5}`, using the same `stablePath` the other installers use.
- Events: sessionStart, userPromptSubmitted, postToolUse, postToolUseFailure, notification, agentStop, errorOccurred, subagentStart, subagentStop, sessionEnd.

### Hook capture (`driga hook copilot <event>`)

- Reads at most the existing hook payload limit from stdin, appends one spool event, never writes stdout, always exits 0, logs failures to the hook log like the other adapters.
- Records the parent process ID and its start time (the hook runs via `exec`, so its parent is the Copilot process). That links the session to a process for liveness, TTY and tmux jump. Call: author's; if the parent turns out to be an intermediate process on a real run, walk up to the nearest `copilot` process.
- Capture policy applies as for Claude: `none` keeps event kinds, timestamps and reasons only; `titles` adds tool names; `snippets` adds the scrubbed, bounded prompt title, tool detail and notification message.
- Error capture retains the event kind and recoverability metadata. Only `snippets` adds a short scrubbed `error: <name>` reason; `titles` keeps no error text. Error messages, contexts and stacks are never stored; stacks are discarded while reading the payload.

### State mapping

| Copilot event | driga meaning |
|---|---|
| sessionStart | records session start and resume source; initializes idle only without existing turn state; never resets an open turn, wait or completion |
| userPromptSubmitted | turn starts: working; ends any open wait |
| postToolUse | activity: working; carries tool name; ends permission and elicitation waits |
| postToolUseFailure | activity: working; ends permission and elicitation waits |
| notification permission_prompt | wait starts, reason permission |
| notification elicitation_dialog | wait starts, reason elicitation |
| notification agent_idle, agent_completed, shell_* | not a wait; activity only (as Claude's idle_prompt) |
| agentStop | turn finished |
| errorOccurred, recoverable=false | turn finished with error; ends any wait; remains finished instead of aging into stalled |
| errorOccurred, recoverable=true | turn finished with error while no later same-session turn activity exists; later prompt, tool result/failure or subagent activity resumes working; sessionStart and passive notifications do not imply recovery |
| subagentStart / subagentStop | activity on the same session (no separate session in v1); can resume a recoverable error, but never clears a wait |
| sessionEnd | session ended |

Inbox lifecycle, alerts and fleet sync reuse the existing rules unchanged: one wait per session and wait start, ended by same-session activity or a user signal, never by another session.

### Processes and sessions

- Process signature for the `copilot` executable, with `-p`/`--prompt` runs as non-interactive. Added to the signature data file, not code.
- Session identity is `sessionId` from hooks. A Copilot process seen without any hook event shows as a process-level session (like agy) with state unknown, and `driga doctor` suggests installing hooks.
- Resume command: `copilot --resume <sessionId>`.
- Title: the first prompt under `titles` or `snippets`; otherwise the project name.

### Tool name on permission waits

The permission notification has `message` and `title` but no tool field. v1 takes a tool name only if the message names it (same rule as Claude's message parser), otherwise none. It does not guess from the last postToolUse. Revisit once a real payload is captured.

## Testing

- Unit tests with payload fixtures copied from the documented schemas, in temp dirs with `DRIGA_DATA_DIR`, `DRIGA_SPOOL_DIR`, `DRIGA_CONFIG` and a temp HOME and `COPILOT_HOME`. Never touch the real `~/.copilot`.
- Installer tests: writes only `driga.json`, respects `COPILOT_HOME`, idempotent, uninstall removes only its file, doctor detects a stale path.
- Capture tests: no stdout, exit 0 on malformed input, oversize input and missing fields; per-policy fields; parent PID recorded.
- State tests: the table above, including agent_idle not being a wait and postToolUse ending a permission wait. CLI 1.0.100 regression fixture preserves the captured order `15:22:05.061 userPromptSubmitted`, `15:22:05.133 sessionStart source=new`, `15:24:06.319 sessionEnd reason=user_exit`; an explicitly synthetic errorOccurred is inserted before exit to test the newly installed hook. Prompt before start stays working; either error kind closes the turn and never becomes stalled; sessionEnd marks it ended. Test later same-session recovery, isolation from other sessions, wait preservation, error privacy and correct-file doctor status in temp homes.
- Real validation (needs the maintainer's approval, because it installs into the real `~/.copilot/hooks/` and runs a Copilot session): install, run a throwaway Copilot session in a scratch tmux window that triggers a permission prompt, confirm stdin delivery, payload fields, parent PID, the inbox item, the jump, and the wait ending on approval. Payloads captured there are scrubbed of account data before any become fixtures.

## Left to the builder

File layout inside `internal/adapter/copilot` and `internal/hooks`, the spool event shape (reuse the existing one), and doctor wording.

## Revisions after review (2026-10-20, Fable)

These override anything above that conflicts.

1. **Latency, not "never slows".** Copilot runs hooks synchronously; only `notification` is fire-and-forget. Every awaited hook must finish within a 50 ms budget on a warm machine, and `timeoutSec` is 3. driga requires Copilot CLI 1.0.98 or newer (1.0.98 fixed hooks hanging until timeout under Node); `driga doctor` and `hooks install copilot` warn on older versions. The real validation measures wall time per hook.
2. **Titles policy.** Under `titles` the session title is the project name; prompts are stored only under `snippets`, matching the 2026-10-18 design. No new `capture_text = "none"` value: the earlier "none" referred to the fleet `share_text` policy, which is unchanged.
3. **Process link.** Record the parent PID only when the parent's executable or command line matches a Copilot signature (native `copilot` binary or the npm loader `node …/@github/copilot/npm-loader.js`, both added to the signature file). Otherwise the session has no PID and stays live by hook recency, bounded like hosted sessions, instead of being marked dead.
4. **VS Code.** VS Code also loads `~/.copilot/hooks/*.json` and sends its own snake_case payload (`session_id`, ISO timestamps, no notification event). The hook accepts both shapes; snake_case payloads are tagged origin `vscode` and never get a PID link. If VS Code skips `exec` entries, these simply never arrive.
5. **Wait ends.** Hook `postToolUseFailure` too (denied or failed tools). A wait ends on same-session `userPromptSubmitted`, `postToolUse`, `postToolUseFailure`, `agentStop`, `errorOccurred` or `sessionEnd`. `agent_idle` means a background agent went idle and `agent_completed` a background subagent finished; neither is a wait, and no notification updates last activity.
6. **Sandbox.** When Copilot's sandbox is enabled, user hooks run inside it and may be unable to reach driga's spool. `driga doctor` warns when `sandbox.enabled` is set in `~/.copilot/settings.json` and when Copilot processes are seen but no Copilot hook events have arrived (sandbox, `disableAllHooks`, or sessions started before install).
7. **Resume.** `copilot --resume=<sessionId>`.
8. **Exit code.** `driga hook` recovers panics and always exits 0. That matters because exit 2 would deny a `permissionRequest`, which is also why driga never hooks it or `preToolUse`.
9. **Payload size.** Decode the payload without keeping `toolResult`; a large tool result must not drop the event.
10. **Doctor "stale".** Stale means the configured `exec` path no longer exists or is not a driga binary; a different but valid driga path is not stale.
11. **Tool name.** Do not apply Claude's "needs your permission to use" parser to Copilot text. v1 records no tool name on Copilot permission waits.
12. **Uninstall.** Output says running Copilot sessions keep calling the removed hook until restarted (Copilot fails open).
13. **Windows** is a non-goal (driga has no Windows platform layer).
14. Web initials distinguish Copilot from Claude.
15. **Live CLI 1.0.100 lifecycle correction.** `copilot -i '<prompt>'` can emit userPromptSubmitted before sessionStart; sessionStart only records lifecycle metadata and never changes established turn state or refreshes existing activity. Quota/model failures may omit agentStop, so install and capture errorOccurred as a report-only terminal signal. A recoverable error is provisionally finished until further same-session turn activity arrives; no timeout is needed, and passive notifications are excluded. Store only `error: <name>` under snippets and never a stack. The documented schema and unprocessed error hook output were rechecked against the [GitHub hooks reference](https://docs.github.com/en/copilot/reference/hooks-reference#erroroccurred--erroroccurred).
