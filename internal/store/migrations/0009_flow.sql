-- +goose Up
ALTER TABLE events ADD COLUMN metadata TEXT NOT NULL DEFAULT 'null';
ALTER TABLE sessions ADD COLUMN mixer TEXT NOT NULL DEFAULT 'null';

-- +goose Down
ALTER TABLE events DROP COLUMN metadata;
ALTER TABLE sessions DROP COLUMN mixer;
