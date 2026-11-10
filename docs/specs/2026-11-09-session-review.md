# Session review: resource reclamation

Reviewed range: `e1eea1b677b57149c086a7d2aae72038390c1a9f` through
`d73488f66ff13e09c2f1d9e79adeac3f7251c79a`: all eight November 9 commits,
115 changed paths. Follow-up corrections are reviewed separately below.

## Method and coverage

Independent fresh-context Standards and Spec reviewers inspected every handwritten
implementation, test and documentation diff. A separate Astra reviewer inspected
all production changes, the mutation paths and relevant platform contracts, and
selected safety tests in depth. The primary agent reviewed the collector, store,
runtime attribution, receipts, cleanup and dashboard changes and adjudicated
findings. Reviewers used the three November 9 resource specs, the disk-reclaim
design and CONTRIBUTING.md.

Coverage includes cleanup discovery/verification/deletion; process identity,
ownership, consent and signals; shutdown accounting and private durable receipts;
detached-process and simulator review; asynchronous Git collection and SQL input
projections; native platform observations; CLI/presentation; dashboard polling;
and integration fixtures. Generated minified JavaScript was assessed through its
TypeScript source, asset manifest and generated-output checks rather than a
separate semantic audit of the minified bundle.

## Standards

No hard documented standards violations were established. No additional optional
code-smell finding was considered concrete enough to require a change. Historical
issue creation and story-before-code chronology were not independently verified.

## Spec and safety findings

1. **P1: current native ownership missing from shutdown authorization.**
   `internal/actions/orphan_ownership.go` reread stored sessions and target
   directories, but could overlook a newly started agent absent from the database.
   An overlapping idle detached helper could pass verification despite an active
   native agent. The action now uses a shared native ownership check independently
   of ingestion, including unknown-directory refusals and configured signatures.
   Regression tests cover post-consent arrival with no stored session, ancestry,
   aliases, missing metadata, custom signatures and disjoint/foreign workloads.

2. **P2: omitted process rows counted as confirmed exits.**
   `internal/kill/kill.go` treated absence from a successful process snapshot as
   exit. Native snapshots can omit live processes when individual metadata reads
   fail. Astra reproduced a live omitted target being reported stopped, with its
   prior memory counted. Final accounting now requires an explicit observation of
   exit, reuse or zombie state for omitted rows; observation failure remains
   unknown. Tests distinguish live omissions, genuine exit, reuse, zombies,
   permission failures and unavailable observers, including an owned-child check.

3. **P2: runtime samples matched PID rather than collector instance.**
   `internal/collector/runtime_stats.go` could attribute retained measurements to
   a new collector with the same PID during startup. A fresh per-run identity now
   travels through the lock owner and runtime sample. Readers require both PID
   and instance to match. An independent lifecycle regression exercises two runs
   in the same process while blocking the replacement's first publication.

## Validation record

The reviewed committed head passed all CI checks, including Linux CI, macOS and
Linux chat sandbox checks, Windows tests and end-to-end tests, site and CLA.
The prior session also completed full isolated `just check` and `just race`,
including 1,216 catalog tests and 212 UI unit tests. These results describe
`d73488f`; follow-up changes require their own validation.

The eight-commit range passed `git diff --check` and a redacted Gitleaks scan.
Runtime attribution unit, doctor and lifecycle tests passed, including the race
detector. The native ownership regressions failed against the old implementation
and passed after correction; the actions/reclaim/derive packages also passed under
the race detector. Astra's original omitted-live-process reproduction now passes.
Focused platform/kill tests, an owned-child exit check, native and Windows vet,
and the Linux build passed. Independent follow-up source review found no remaining
blocker. The final combined full Go suite and full `just race` both passed through
`scripts/isolated.sh`, with `GOFLAGS=-p=4` limiting concurrent packages on the
native host while retaining race instrumentation and within-package concurrency.
No browser source changed in the follow-up, so its earlier generated-output and
browser-suite evidence still applies; those browser suites were not repeated.

## Remaining limits

- Collector physical footprint measured 61–68 MB, above its 50 MB target; a
  complete sample measured 11.9% of one CPU core. No idle baseline is certified.
- Large-inventory Home/Header API reads exceeded 40 seconds. Polling fixes do
  not establish a startup or refresh latency bound.
- The latest installed desktop smoke attempt could not bind a native window.
  Final native rendering remains unverified.
- Native observation and signaling retain platform check-to-action races. This
  review does not claim atomic signaling, exhaustive workload detection or a
  power-loss durability audit.

No original user workload or simulator was stopped during this review. Synthetic
fixtures and disposable test state provide regression evidence, not proof of
all host-specific behavior.
