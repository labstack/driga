-- +goose Up
ALTER TABLE orphans ADD COLUMN verified INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE orphans DROP COLUMN verified;
