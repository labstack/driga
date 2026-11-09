# Resource reclaim reliability

Driga must help users review disk, memory and CPU opportunities through its native
commands, distinguish protected ongoing work, and preserve action outcomes.

- `driga reclaim` lists local disk candidates, orphan trees and attributed live
  sessions together. Ready actions sort first, followed by sampled CPU, memory
  and disk size. Unknown sizes remain unknown; different resource units are not
  added together. Next steps use existing confirmed actions or open a live session.
- Current session ownership and ambiguous live-session directory overlap prevent
  unattended orphan shutdown. Verify ownership again after consent using current
  process identities and session records; stored evidence cannot authorize signals.
- Process action receipts survive command completion. `driga reclaim history`
  reads bounded local process receipts and existing disk cleanup receipts. Record
  confirmed exits, survivors, uncertainty and prior memory usage. Avoid raw command
  lines, error messages or session text. A journal failure must remain visible
  without overwriting an already performed mutation's result.
- Slow Git inventory must not block process/machine collection, spool handling or
  OTLP intake. Use one cancellable background inventory job with no growing queue;
  retain its result until committed. Preserve synchronous disposable snapshots.
- Reduce measured allocation overhead without changing monitoring coverage or
  increasing the physical-memory budget. Expose phase-correlated Go runtime
  measurements separately from native physical footprint. Runtime sampling must
  be bounded and attributed only to the matching live collector instance.
- Header and smoke integration tests must keep checking the real behavior with
  isolated fixtures. Readiness probes must not repeatedly cancel useful work;
  cleanup assertions must not depend on unrelated processes on the test host.

Active work remains protected. No automatic blanket cleanup, process eviction,
forced garbage collection, or resource-savings claim based solely on signals sent.
Platform process identity rules and existing confirmation requirements still apply.
