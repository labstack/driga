# Interface regression audit — 2026-11-10

Baseline: `62b1ac1d3597bbbf23c60065b3e6ac2e6f38d9b6` (v2026.11.1-alpha).

## Scope and acceptance

Exercise shipping interfaces and follow confirmed failures through reproduction,
regression tests, fixes and independent review. Preserve capture policies,
provider isolation, confirmation requirements and wire compatibility. Tests must
use disposable state and fixture assistants; never signal real workloads or run
real assistant turns. Existing functionality should behave consistently across
its supported interfaces. This is a bounded audit, not proof of absence of bugs.

## Confirmed defects

| Interface | Before | Required result | Regression |
| --- | --- | --- | --- |
| macOS service | Sequential launchctl pipe reads hang status and installer postflight when stdout fills. | Drain both streams concurrently with bounded capture, deadline and owned-child cleanup. Start must use the repaired query. | `TestLaunchdStatus*`, `TestLaunchdStartUsesBoundedStatusQuery` |
| TUI | Global navigation hides an armed stop confirmation; later Enter can stop the old selection. | Cancel stop before navigation; subsequent Enter cannot confirm it. Mouse navigation cancels too. | `interface_audit_test.go` |
| TUI | Lowercase k is swallowed on ordinary tables. | j/k navigate down/up while process-specific actions retain their meaning. | `TestAuditTableVimUpNavigation` |
| Desktop | Back/Forward do not restore page/dashboard selection; a deleted-dashboard fallback can trap Back on the missing entry. | Browser history and selected page stay synchronized; 404 fallback replaces the invalid entry. | `App.audit.test.tsx`, App stories |
| Desktop Chat | Header/pane close bypasses mode reset; loading or pending elevation can leave Full Auto enabled. | All close paths reset mode before accepting close; unresolved state blocks close and failed reset restores the pane. | App audit tests and browser close stories |
| Desktop Search | Missing clipboard API silently drops the resume command. | Show a usable fallback command when clipboard is absent or rejects. | `Search.audit.test.tsx`, Search stories |
| Desktop dashboard | Transient loading errors discard saved-dashboard selection. | Only a confirmed 404 returns Home; retryable errors retain selection. | dashboard refresh tests and App stories |

The large-output service test failed before its fix within two seconds. TUI and
desktop regressions likewise failed against baseline behavior. The repaired
read-only native service query returned `running` outside the macOS sandbox,
using disposable application state. No production service was restarted for
this audit.

## Coverage and limits

| Area | Evidence | Remaining live coverage |
| --- | --- | --- |
| CLI | Full Go suite; parsing, scopes, JSON, confirmations and action fixtures | Real destructive reclamation intentionally excluded |
| TUI | Full Go suite, app-update key/mouse regressions and fake action seams | Interactive native terminal rendering and terminal jumps |
| Desktop/browser UI | Unit tests, shipping-component Storybook plays, typecheck and production build | Installed native window, menus, tray, dialogs and OS notifications |
| MCP and web API | Tool/schema/envelope, capture, origin/identity and confirmation fixtures | Third-party live MCP hosts |
| Chat | Selection, pinned resumes, streaming, approvals, queue/history and reconnect fixture runners | Real Claude/Codex/Copilot authentication and model turns |
| CPU/memory/reclaim | Canonical process placement, current identity, exclusion and stale-data fixtures; derive/reclaim suites | Linux/Windows native process APIs and real reclamation |
| Fleet | Machine-scoped paths/readers, freshness and protocol fixtures | Live multi-machine transport |
| Hooks, intake, store and collectors | Full Go suite and targeted provider/self-exclusion/accounting checks | Real vendor configuration and histories intentionally excluded |
| Packaging/platform | Native build, native and Windows vet | New signed release and Windows runtime not exercised here |

Project-summary actions remain Claude-specific by existing design; provider
selection does not imply every provider has Claude's native tool catalog.
Documentation now describes capture/vendor history for the selected assistant
and the actual sixteen-metric header limit.

## Verification and review

Passed on macOS with disposable HOME/data through `scripts/isolated.sh`:

- `go test ./... -count=1` and `go test -race ./...`.
- Final affected TUI/service tests and TUI race tests after wheel cancellation;
  launchd race tests include timeout cleanup and Start dispatch.
- `go vet ./...`, `GOOS=windows go vet ./...`, and a Linux CLI build.
- Final `go test ./internal/web ./internal/desktop -count=1` after bundle rebuild.
- `go mod tidy -diff`, generated config comparison and docgen `--check`.

UI unit suite: 219 tests across 32 files passed; all seven App audit tests passed
after the final history fix. Typecheck and both production bundles passed.
Focused App/Search browser plays passed (32), then the new deleted-dashboard
history story passed. Gitleaks found no secrets in the staged changes. Source
diff whitespace checks passed; generated bundles retain dependency license text.

The full Chromium catalogue passed: 1,266 tests across 172 files, covering both
themes and both review widths. The final history regression was added during
that run; affected history plays passed again across all four cells (16 tests
in four files) against the frozen final source, instead of relying on which
cell loaded the change during the broader run.

An early race attempt and Windows vet overlapped a bundle rebuild and failed on
temporarily absent embed files. Verification was rerun after the build; the race
suite and Windows vet passed. One UI unit attempt encountered a shared Vitest
cache import error; its serialized rerun passed. These were not product fixes.

### Standards

Independent fresh-context review found one P1: local mode could be stale while
loading or enabling Full Auto. The fix refuses closure until initialization and
mode changes settle, then requires an acknowledged Confirm reset. Rereview found
zero remaining documented-standard violations or actionable smell findings.

### Spec

Independent fresh-context review found the same pending-elevation race and a
mouse-wheel cancellation gap on the flow board. Both received failing regression
tests and fixes. The reviewer's separate real-Chat pending-elevation reproduction
changed from failing to passing. Final rereview found zero outstanding findings.

### Astra consultation

A separate Astra pass reproduced a Back-history trap after a saved dashboard
was deleted elsewhere: fallback navigation pushed Home again, causing the next
Back to hit the deleted dashboard repeatedly. The regression requires fallback
navigation to replace that invalid entry while intentional navigation pushes.
Its independent reproduction passed after the fix; Astra reported no additional
blockers. The installed signed release was not replaced with this audit build.
