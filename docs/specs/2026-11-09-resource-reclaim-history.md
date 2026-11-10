# Resource reclaim action history

Process shutdown results must survive the command that requested them. The
shared action executor records each attempted candidate, including refusals,
cancellation, partial shutdown and unknown final process observations. This
covers terminal, TUI and app actions that use the shared executor.

`driga reclaim history` shows local process receipts alongside existing disk
cleanup receipts. Use `--kind process` or `--kind disk`, `--limit 50` (1–1000),
and `--json` for structured results. Receipts are newest first. The JSON data
contains `entries` and optional history read warnings.

Process receipts retain candidate and machine IDs, start/completion timestamps,
root PID, status, signaled/stopped/surviving/unknown PIDs, structured error code,
last observed CPU and process memory accounting before/after shutdown. Memory
held by exited processes is historical accounting; it does not measure an
increase in available system RAM. An unknown final observation is never counted
as stopped. CPU is the last observation, not a measured reduction in host CPU.

Receipts deliberately omit command lines, arguments, cwd, session text and error
messages. Disk history projects only ID, timestamp, status, dry-run and observed
filesystem free-space delta from cleanup logs; it does not expose saved commands
or paths. Legacy disk receipts lack machine identity and action start time.

Process receipts append to private `reclaim.log` in the Driga data directory.
The existing cross-process locked log writer bounds this to 10 MiB plus one
10 MiB rotated file. Each receipt is limited to 64 KiB. Successful writes are
synchronized under the cross-process file lock before the executor returns.
Rotation synchronizes the retained copy before truncating the original; a copy
sync failure preserves the original receipts and reports a history warning.
Ordinary diagnostic logs retain their existing asynchronous writes.
Rotation follows the shared diagnostic
log mechanism; this is a bounded local journal, not an archival audit database.
History reads at most 10 MiB from each retained process/disk log and retains at
most 1000 entries. Incomplete crash tails or malformed records produce warnings
while preserving earlier readable history. Reading never starts the collector,
invokes tools or opens the database for writing.

A history write failure adds a visible warning to the returned action report.
The original mutation result and action error remain intact: a completed stop
must not become a failed stop because its receipt could not be written. A crash
between signaling and writing a receipt can still leave an unrecorded action;
there is no replay or automatic retry of mutations. History is local and is not
replicated through fleet feeds.

Validation covers cancellation through the shared executor, distinct history
append failure, privacy of partial results, concurrent appends, malformed tails,
disk-history projection, bounded record size and the CLI JSON/argument surfaces.
