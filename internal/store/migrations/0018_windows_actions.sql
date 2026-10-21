-- +goose Up
ALTER TABLE orphans ADD COLUMN process_starts TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE orphans DROP COLUMN process_starts;
