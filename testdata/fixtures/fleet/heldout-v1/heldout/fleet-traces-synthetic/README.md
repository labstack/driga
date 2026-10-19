# Synthetic fleet traces (held out)

Hand-authored scenario, not captured from a user's machine. Fixed clock:
2026-10-18T12:00:00Z. One peer, `trace-peer`, runs a Claude session `build`
and a hosted Codex thread `review`. Labels in expected.json were reasoned from
this scenario and the v1.5 design §§13–14 before applying the trace. Do not
regenerate expectations from program output or tune rules to this set.

1. A snapshot at epoch A, seq 10 reports build working and review ~idle.
2. Seq 11 reports build waiting; contact at +1 second keeps the peer live.
3. Replaying seq 11 with contradictory finished content is a duplicate: ignore
   its content, preserve waiting and the durable cursor.
4. Attempt seq 12 (finished), then seq 11 (dead) in the same batch. Reject the
   decreasing sequence and roll back the earlier write and cursor atomically.
5. Attempt a finished row with no epoch. Reject; keep waiting.
6. Attempt a finished row under a different epoch. Require a fresh snapshot;
   keep the existing rows and cursor.
7. Attempt seq 13 with seq 12 missing. Reject without silently skipping data.
8. At +45 seconds from the last contact (+1s), the peer is stale. Its last-known
   session labels remain waiting and ~idle: lack of transport is not evidence
   that a remote agent died.
9. A new-epoch snapshot at +47s replaces the rows, with only build finished.
   New contact restores live state. This is replacement, not a union.
