Synthetic Linux VM/container scenario, written for Task 2; no real agent capture.

Clock: 2026-10-18 18:00 UTC. UID 1001 has systemd --user PID 700.
Claude 7100 has matching registry/start identity and a busy prompt 20 seconds
ago: working. Codex 7200 is a live CLI with a task started 20 seconds ago:
working. Each session occupies its own repository; no file edits or conflicts.
The mature, idle broker 7300 (started 16:00 UTC, Unix 1792339200) and its child
7301 were reparented to the user manager. No session owns that tree: one orphan,
with reparented to systemd --user evidence. Snapshot verification remains false.

proc/ deliberately has no pressure/memory, as in containers without PSI.
Its meminfo gives MemAvailable/MemTotal = 25%; PSI must remain unavailable.
Registry fields are synthetic assumptions, not evidence of Linux agent output.
Expected labels above were reasoned by hand before collection, using the agent-state, orphan,
and Linux process-sampling rules. Do not rewrite expected.json from Driga output.
Empty Git objects are shared input scaffolding from the existing synthetic corpus.
