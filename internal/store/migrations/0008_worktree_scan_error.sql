-- +goose Up
ALTER TABLE worktrees ADD COLUMN scan_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE worktrees DROP COLUMN scan_error;
