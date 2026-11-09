# Detached work and simulator review

Resource review must expose detached work even when it does not match an agent
signature. It must distinguish a review opportunity from permission to stop it.

- Preserve independently readable process arguments and executable paths. Retry
  incomplete identity observations; reject observed image changes, never merge
  partial fields from separate attempts, and never use a kernel command name as
  proof of an executable path.
- `driga reclaim` includes local detached-process review and booted iOS simulator
  observations alongside existing confirmed orphan/session and disk opportunities.
  `--processes` skips disk and simulator discovery; `--simulators` skips disk and
  process discovery. Combining them reviews both. `--pid PID` inspects that exact
  current process, including protected or incomplete records, without scanning
  disk or invoking agent CLIs. Missing PIDs are reported as absent from that
  observation, not silently omitted from a ranked top-N list.
- General process review is read-only. Detached topology, age, small trees,
  temporary data paths and low sampled CPU are evidence for human review, not
  automatic shutdown authority. Existing confirmed orphan actions retain their
  fresh identity, activity, ownership and consent requirements.
- Current user identity, system/application/service protection, live session
  ownership and directory overlap, CPU sample availability and socket observation
  availability are explicit. Active shells and their sleeping children remain
  protected. Unknown metadata never means idle or safe.
- Use broad executable/provenance rules and held-out fixtures, never PID lists,
  project names, port numbers or thresholds derived from this workstation.
  A database with temporary data-directory provenance needs workload review;
  ordinary databases, process supervisors and device services remain protected.
- Simulator inventory uses one bounded read-only `xcrun simctl list` request on
  demand. Report exact device identity and known last-use/boot timestamps
  separately. Last use does not establish boot age or present idleness. Missing
  observations remain unknown; no shutdown or resource-savings claims follow.
- Process-only and simulator-only inspection never starts the collector, reads
  agent transcripts, walks Git repositories or writes the persistent database.
  Stored session evidence remains on one read transaction and is labelled stale
  or unavailable when it cannot establish present ownership.

Validation uses isolated synthetic process trees, metadata failures, active
sessions, databases, dev servers, six-device inventories and exact-PID lookups.
Native validation reads the real product inventory without stopping workloads.
