-- +goose Up
-- Only the local numeric session identity is retained, never permission input.
ALTER TABLE prompt_intervals ADD COLUMN session_id INTEGER NOT NULL DEFAULT 0;
