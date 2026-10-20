-- +goose Up
-- New observation instants use UTC Unix milliseconds.
CREATE TABLE observation_minutes (
 machine_id TEXT NOT NULL,
 session_id INTEGER NOT NULL,
 minute INTEGER NOT NULL,
 log_bytes INTEGER NOT NULL DEFAULT 0,
 cpu_ms INTEGER NOT NULL DEFAULT 0,
 samples INTEGER NOT NULL DEFAULT 0,
 last_growth_at INTEGER NOT NULL DEFAULT 0,
 last_growth_bytes INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY (machine_id, session_id, minute)
) WITHOUT ROWID;
CREATE INDEX observation_minutes_session_minute ON observation_minutes(session_id, minute);

-- +goose Down
DROP TABLE observation_minutes;
