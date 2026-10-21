-- +goose Up
ALTER TABLE sessions ADD COLUMN started_from TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN started_from;
