-- +goose Up
ALTER TABLE sessions ADD COLUMN last_decision_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN last_api_request_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN last_api_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_api_error;
ALTER TABLE sessions DROP COLUMN last_api_request_at;
ALTER TABLE sessions DROP COLUMN last_decision_at;
