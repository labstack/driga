-- +goose Up
ALTER TABLE procs ADD COLUMN partial INTEGER NOT NULL DEFAULT 0;
ALTER TABLE machine_samples ADD COLUMN commit_used INTEGER NOT NULL DEFAULT 0;
ALTER TABLE machine_samples ADD COLUMN commit_limit INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE machine_samples DROP COLUMN commit_limit;
ALTER TABLE machine_samples DROP COLUMN commit_used;
ALTER TABLE procs DROP COLUMN partial;
