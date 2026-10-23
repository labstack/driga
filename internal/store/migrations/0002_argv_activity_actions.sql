-- +goose Up
-- Exact argv for process identity; cmdline stays the display string.
ALTER TABLE procs ADD COLUMN argv TEXT NOT NULL DEFAULT '[]';
-- Live sessions in activity order without a temporary sort.
CREATE INDEX sessions_active_activity
ON sessions(machine_id,last_activity_at DESC,id)
WHERE ended_at IS NULL;
-- Chat action cards keep their tool and decider across restarts.
ALTER TABLE messages ADD COLUMN tool TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN decided_by TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE messages DROP COLUMN decided_by;
ALTER TABLE messages DROP COLUMN tool;
DROP INDEX sessions_active_activity;
ALTER TABLE procs DROP COLUMN argv;
