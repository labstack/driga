-- +goose Up
-- Exact argv for process identity; cmdline stays the display string.
ALTER TABLE procs ADD COLUMN argv TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE procs DROP COLUMN argv;
