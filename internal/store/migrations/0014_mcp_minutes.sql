-- +goose Up
CREATE TABLE mcp_minutes (
    machine_id TEXT NOT NULL,
    minute INTEGER NOT NULL,
    host_pid INTEGER NOT NULL,
    host_start INTEGER NOT NULL,
    command_key TEXT NOT NULL,
    sampled_at INTEGER NOT NULL,
    host_name TEXT NOT NULL,
    host_exe TEXT NOT NULL,
    host_cwd TEXT NOT NULL,
    name TEXT NOT NULL,
    count INTEGER NOT NULL,
    footprint INTEGER NOT NULL,
    members TEXT NOT NULL,
    PRIMARY KEY(machine_id, minute, host_pid, host_start, command_key)
);

-- +goose Down
DROP TABLE mcp_minutes;
