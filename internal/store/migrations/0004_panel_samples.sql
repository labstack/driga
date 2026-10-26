-- +goose Up
ALTER TABLE machine_samples ADD COLUMN disk_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE machine_samples ADD COLUMN cpu_percent REAL;
-- Labels are overwritten on each pass; timestamps alone cannot recover a
-- past inferred stuck state or a machine's exact poll sequence.
CREATE TABLE panel_polls (
 machine_id TEXT NOT NULL,
 ts INTEGER NOT NULL,
 worst TEXT NOT NULL,
 stuck INTEGER NOT NULL,
 PRIMARY KEY(machine_id,ts)
) WITHOUT ROWID;
-- Permission inputs remain memory-only. These intervals contain just a random
-- prompt ID and timestamps so the attention count includes Chat approvals.
CREATE TABLE prompt_intervals (
 machine_id TEXT NOT NULL,
 id TEXT NOT NULL,
 started_at INTEGER NOT NULL,
 ended_at INTEGER,
 PRIMARY KEY(machine_id,id,started_at)
) WITHOUT ROWID;
-- The raw tail is capped at 200 events/session. Keep the content-free evidence
-- needed for a complete day of counts and the last 30 retained turn outcomes.
CREATE TABLE panel_events (
 machine_id TEXT NOT NULL,
 adapter TEXT NOT NULL,
 external_id TEXT NOT NULL,
 ts INTEGER NOT NULL,
 kind TEXT NOT NULL,
 metadata TEXT NOT NULL DEFAULT 'null',
 summary TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(machine_id,adapter,external_id,ts,kind)
) WITHOUT ROWID;
CREATE INDEX panel_events_ts ON panel_events(machine_id,ts);
-- Recover the metadata the existing raw tail still contains at upgrade.
INSERT INTO panel_events(machine_id,adapter,external_id,ts,kind,metadata)
SELECT machine_id,adapter,external_id,ts,kind,
 CASE WHEN json_extract(metadata,'$.tool') IS NOT NULL
 THEN json_object('tool',json_extract(metadata,'$.tool')) ELSE 'null' END
FROM events WHERE kind IN ('UserPromptSubmit','userPromptSubmitted','otlp:user_prompt',
 'Stop','agentStop','errorOccurred','SessionEnd','sessionEnd',
 'PostToolUse','PostToolUseFailure','postToolUse','postToolUseFailure','otlp:tool_result')
GROUP BY machine_id,adapter,external_id,ts,kind;
