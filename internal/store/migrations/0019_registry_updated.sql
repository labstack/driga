-- +goose Up
ALTER TABLE sessions ADD COLUMN registry_updated_at INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN registry_updated_at;
