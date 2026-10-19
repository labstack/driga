-- +goose Up
CREATE TABLE summaries (
    machine_id TEXT NOT NULL,
    project_id INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    input_hash TEXT NOT NULL,
    state_hash TEXT NOT NULL,
    text TEXT NOT NULL,
    model TEXT NOT NULL,
    used_snippets INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(machine_id, project_id, used_snippets)
);

-- +goose Down
DROP TABLE summaries;
