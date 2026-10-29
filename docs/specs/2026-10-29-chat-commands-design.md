# Chat runs commands, and approvals scale

Date: 2026-10-29. Status: approved by the owner for implementation.

## Intent

Chat should do what a developer would do at the terminal, through one
general mechanism, without a purpose-built tool per case. Today it cannot.
A user asked Chat to stop 65 duplicate `maestro.cli.AppKt mcp` MCP server
processes. Chat diagnosed the problem correctly, had no tool for it, and
told the user to run `pkill` themselves. The owner rejects both that
outcome and the alternative of inventing a tool per incident.

Chat's shell was removed on purpose (`60f39c4`, "Chat reads files but
never runs commands or writes them"). The reason was that a single Allow
could run any command the model wrote. This design brings command
execution back without reopening that hole.

The same owner hit a related problem from the other side: 9 approval
prompts for one cleanup task. The second part of this design lets one
approval cover a bounded, visible set of similar actions.

## Goals

### Part A — one command gateway for Chat

1. **One Driga tool, `run_command`.** Chat calls it with either a literal
   `argv` (preferred) or an explicit shell `script`, plus `cwd` and
   `timeout_seconds`. There are no other new action tools. The broker,
   not the model, executes.
2. **Every request goes through prepare → authorize → execute → record**,
   including requests that are approved automatically.
   - **prepare:** parse, classify, resolve targets, and build the card.
   - **authorize:** a mode rule, or the person through a card.
   - **execute:** consumes a single-use authorization bound to a hash of
     the prepared request.
   - **record:** stores the result with the conversation's action log,
     under the existing capture policy and retention.
3. **The sandbox is the default.** Commands run inside an OS sandbox:
   - writes are allowed only to the conversation's selected project
     directory, a per-conversation scratch directory, and the Git metadata
     directories needed by a selected worktree;
   - every writable root protects execution persistence paths: `.git/hooks/`,
     `.git/config` (including hooksPath, fsmonitor, aliases and filters),
     `.git/config.worktree`, `.git/info/attributes`, `.gitattributes`, `.envrc`,
     `.vscode/`, `.idea/` and `.husky/`. Existing nested repositories, resolved
     worktree gitdirs and the common Git directory get the same protections.
     Gitdir/commondir pointer files cannot be replaced. Missing protected paths
     cannot be created. Ordinary Git state (objects, refs, index, HEAD and logs)
     remains writable, so staging and committing continue to work;
     protected-path symlinks and nested gitdir pointers that would widen the
     selected write scope are refused;
   - there is no network unless the card granted it;
   - reads of Driga's data, config and approval secrets, and of agent
     credential stores (`~/.claude*`, `~/.codex`, `~/.ssh`, keychains,
     cloud credential directories), are denied;
   - the environment is stripped to a safe allowlist (PATH, HOME, LANG,
     TERM, TMPDIR and the like), with no tokens or keys.
4. **Host process control is a command family, not a tool.** When the
   parsed argv is `kill`, `pkill` or `killall` (and on Windows
   `taskkill`, `Stop-Process`), the gateway:
   - resolves the targets itself into a snapshot of PID, owner, start
     time, executable and command line, using the existing process
     collector;
   - shows them on the card;
   - after Allow, signals exactly that snapshot by identity, re-checking
     each process immediately before signalling and skipping any that
     changed;
   - never runs a fresh pattern match.

   It reports the processes that were stopped, had already vanished, were
   skipped, or survived. SIGTERM is the default. SIGKILL or force needs a
   second card. Processes not owned by the user, PID 1, process groups
   (`kill 0`, negative PIDs, `kill -1`), Driga's own processes and the
   Chat session's own process tree are always refused.
5. **The command policy is keyed on parsed argv, not on strings.**
   - **Read** (auto-allowed in Auto and Full auto): a recognized
     read-only executable with validated flags, for example `ls`, `cat`,
     `head`, `tail`, `wc`, `ps`, `pgrep`, `lsof`, `du`, `df`, `uptime`,
     `rg`, `grep`, `find` without `-exec`/`-delete`/`-ok`, `git`
     `status|log|diff|show|branch --list`, `stat`, `which`. Pipes are
     allowed only between such reads. Any ambiguous syntax
     (substitution, redirection to files, `eval`, unknown flags) is not a
     read.
     The cwd and every file/path operand must resolve with `EvalSymlinks`
     inside the project or scratch. Missing paths and symlinks escaping those
     roots do not qualify. Recognized reads outside that boundary become
     **Other with an individual-card requirement**, including in Full auto;
     similarity grants cannot approve them. For example, `cat ~/notes.txt`
     always needs a card, while `cat README.md` can be automatic when the file
     resolves inside the project. Commands with no path operands, such as
     `ps`, `df` and `uptime`, retain Read when their cwd is in scope.
   - **Process** (always carded, any mode): the kill family above.
   - **Other:** builds, tests, package managers, git writes, file
     changes, scripts. These run sandboxed. They get a card in Confirm
     and Auto. In Full auto they run without a card inside the sandbox,
     except destructive ones and reads outside the project/scratch boundary
     (see above).
   - **Destructive** (always carded, any mode): `rm` outside scratch,
     `git push|reset --hard|clean|branch -D|checkout -- .`,
     `docker rm|rmi|volume rm`, writes to paths outside the project, and
     anything that asks for network.
   - **Never** (refused with a reason, no card): `sudo`, `su`, `doas`,
     `pkexec`, `launchctl`/`systemctl` writes, `chmod`/`chown` on
     protected paths, any request to run unsandboxed, and any change to
     Driga's own settings, hooks or approval state.
6. **Cards say what will happen in plain words.** "Stop 65 processes
   owned by you matching `maestro.cli.AppKt mcp` (about 5 GB)", with the
   targets in an expandable list. For other commands the card shows the
   exact argv or script, the cwd, "sandboxed: writes only to <project>,
   no network", and the classification.
7. **Full auto changes.** Today it approves everything. It now approves
   reads and sandboxed "other" commands. Process, destructive actions and
   reads outside the project/scratch boundary still need a card.
8. **Delegated approvals are contained.** When Chat answers another
   agent's permission prompt (`answer_prompt`), it can never answer
   "allow" automatically in any mode. The person approves through a
   card, and the card shows the other agent's exact tool input.

### Part B — one approval for similar actions

9. **Grants.** A card can offer "Allow similar for this conversation"
   (Chat) or "Allow similar for this session" (another agent's prompt,
   answered in the app). A grant is the classified command family plus a
   pattern over argv, for example `git worktree remove <path under
   project>`. It expires at the end of the conversation or session, or
   after 30 minutes, whichever comes first.
   - Grants are never offered for Process, Destructive or Never
     requests.
   - A grant is created only by a click in the app, never by model text.
   - Each auto-allowed action under a grant shows in the timeline as
     "allowed by your grant", with a link to revoke the grant.
10. **Bursts are grouped.** Pending prompts from one session that arrive
    within 3 seconds show as one card with an "Allow all N" button. Each
    item is still listed and individually deniable.

## Non-goals

- Interactive terminals and long-running servers started by Chat. A
  timeout ends commands; the default is 120 s and the maximum 600 s.
- Commands on paired or remote machines. The current rule stays: actions
  run on this machine only.
- Suggesting permanent allow rules for Claude Code's `settings.json`.
  That is a later step.
- General undo. Process termination and network effects cannot be undone.

## Anti-goals

- A per-incident tool (`stop_maestro`, `kill_pileup`, …).
- Any path where text from transcripts, file names, process arguments or
  repository files creates or widens permission.
- A card that hides what will actually run, or a preview that differs
  from what executes.
- Silent fallback to running unsandboxed when the sandbox is unavailable.
  That state is an error the card states, and only the kill family and
  reads remain available.

## Platform

- **macOS:** Seatbelt via `sandbox-exec` with a generated profile. The
  builder validates it on macOS 15+ and records the tested versions.
  Protected paths receive `deny file-write*` rules after all write allows.
- **Linux:** `bubblewrap` (`bwrap`) when it is installed. Without it, the
  sandbox is unavailable (see the anti-goals) and the card and
  `driga doctor` say so.
  Protected paths are rebound read-only after writable roots. Absent paths
  use empty read-only mounts at their first missing ancestor, with temporary
  host mountpoints removed after execution. An absent `commondir` receives a
  read-only `.` self-reference so ordinary repositories keep working.
- **Windows:** no sandbox in v1. Only reads and the process family are
  available. Other commands are refused with "Running commands needs a
  sandbox, which Windows does not have yet."
- Anthropic's standalone `sandbox-runtime` was considered. It needs
  Node, and Driga ships as a single Go binary, so v1 builds the same
  approach natively. The builder may revisit this if the native profiles
  prove unreliable.

## Constraints

- **Reuse the existing pieces:** the Chat broker's approval path
  (`internal/chat/actions.go`, `Approve`/`resolve`/action cards), the
  trust modes, the MCP `--chat` server, the process collector and the
  kill path in `internal/kill`. No parallel approval system.
- Chat still starts Claude with only Read, Glob, Grep and TodoWrite as
  built-in tools. Bash stays off, and Chat still refuses to start if
  Claude offers it. Commands go only through `run_command`.
- Libraries over hand-rolled code where one exists. Use a Go shell
  parser such as `mvdan.cc/sh/v3/syntax`, not a regex.
- **Tests:**
  - use fake processes and fake agent CLIs, never real agents;
  - never touch the real `~/.claude` or `~/.codex`;
  - prove each test fails without its fix;
  - run end to end on the built binary: Chat (fake Claude) calls
    `run_command` with `pkill -f <pattern>` against spawned dummy
    processes, the card lists exactly those, Allow stops them, and a
    process spawned after the snapshot survives;
  - a sandbox test proves a write outside the project and a network
    connection both fail on macOS and Linux CI.
- The TUI and the app show the same cards in the same words.

## Left to the builder

- The exact read allowlist and its flag validation, as long as it is
  conservative and tested.
- The seatbelt and bwrap profile details, within the boundaries above.
- Card layout, within the existing card components.
- How results are truncated in the transcript (keep head and tail, store
  the full output under the capture policy).
