# Chat command validation

Date: 2026-10-29. Implementation reference:
[Chat commands design](../specs/2026-10-29-chat-commands-design.md).

## Reproduce

Use a disposable HOME/config/data/spool, never an installed agent CLI:

```sh
scripts/isolated.sh go test -tags e2e ./cmd/driga -run '^TestChatCommandsBuiltBinary$' -v -count=1 -timeout 25m
DRIGATEST_REQUIRE_SANDBOX=1 scripts/isolated.sh go test ./internal/cli ./internal/command -run 'Test(DoctorSandbox|CommandSandboxIntegration)' -v -count=1
```

Built-binary tests follow this repository's `cmd/driga/service_chat_test.go`
and `smoke_test.go` pattern rather than adding Unix cases to the Windows-only
`internal/e2e` suite. They build Driga, fake Claude and a process helper. The
fake implements capability probes and stream-json, launches the actual built
`driga mcp --chat` from its generated config, and calls `run_command` over MCP.
The client uses the service's authenticated socket. All homes, config, data,
spool and selected project paths are private. No real agent runs.

The Unix snapshot case starts **65** owned children, asserts that the card
contains exactly those complete identities, then starts child **66** with the
same unique pattern. Allow must stop the 65 and leave 66 alive. A separate
case checks stopped, vanished (fixture exits through stdin), skipped
(fixture execs with changed argv), and surviving (fixture ignores TERM)
identities. Only the survivor appears on the second force card. Cleanup
signals only direct fixture children and reaps only its own `exec.Cmd`s.
Additional cases cover Deny, Stop, automatic reads, Full auto Process and
Destructive cards, one-shot replay, delegated allow, grant revocation and
frozen burst membership. Windows uses literal `taskkill /PID`, its native
close/force lifecycle and the exact unavailable-sandbox refusal.

## Local observations and limits

Environment: macOS **27.0.1**, build **26A434**, Darwin **27.0.0**,
arm64; Go **1.27.3**. Seatbelt backend: `/usr/bin/sandbox-exec`, supplied
by that OS (no independent backend version).

This agent session is itself restricted. Seatbelt's initialization probe
returns **exit status 71**, so the optional native integration test skips.
With `DRIGATEST_REQUIRE_SANDBOX=1`, it must fail, not skip. Native process-list
inspection returns **operation not permitted**, preventing completion of the
strict process E2E cases here. These are environment failures, not red-test
evidence and not proof of working sandbox boundaries. No native denial
claim is made for this machine, Linux or Windows from this session.

The doctor tests inject available/failed Seatbelt, available/missing/unusable
bwrap, and Windows capabilities. They verify status, backend/OS, reason and
remediation. Built-binary automatic reads, request replay, grant/revoke,
delegated allow and frozen bursts pass even without a usable sandbox.

TDD evidence: the new doctor test first failed to compile because
`DoctorDeps.SandboxProbe` was absent. Removing the diagnostic registration
then fails with a missing `chat_sandbox` check; restoring it passes.
Temporarily disabling the broker's
`/run-command` gateway makes the built-binary automatic-read assertion fail
(no action); restoring it passes. Temporarily omitting process target
preparation makes the 65-process card-size assertion fail before any Allow.
These regression mutations are restored, never committed.

## Native CI evidence

The `chat-sandbox` matrix in `.github/workflows/ci.yml` runs on **macos-15**
and **ubuntu-24.04** with `DRIGATEST_REQUIRE_SANDBOX=1`. Those are requested runner
labels, not claims of tested point releases. The version step records exact
`uname -a`, `sw_vers` or `/etc/os-release`, backend path/version, relevant
sysctls and `go version` in the job log. Native job results are pending until
CI runs; this branch is not pushed by this task.

The Linux job installs `bubblewrap`, probes namespace creation, and explicitly
sets `kernel.apparmor_restrict_unprivileged_userns=0` **only if present on its
disposable runner**. Ubuntu's AppArmor user-namespace restriction can otherwise
reject bwrap despite installation. This is CI setup, not a product fallback
or an instruction to weaken a user's host. Driga only diagnoses the condition.

The descendant-cleanup fixture checks its unique generated executable in
the host process list. It never mistakes bwrap's namespace-local printed PIDs
for host PIDs, and cleanup rechecks each fixture identity's start time.

`TestCommandSandboxIntegration` logs the capability/OS and Linux bwrap version,
then executes assertions for project/scratch writes, denied outside/symlink
writes, protected reads, host signalling, stripped environment, default TCP
and DNS denial, Unix-socket denial even with a network grant, explicit TCP
grant success, and child/grandchild timeout cleanup. Required capability
failure is fatal. Do not replace those checks with profile-string assertions.

The existing `windows` job also explicitly runs native ownership collection,
snapshot identity/signalling, process-family/protection, unavailable-sandbox
and doctor tests, plus the built-binary Windows cases. Cross-compilation/vet
is not presented as native Windows execution evidence.
