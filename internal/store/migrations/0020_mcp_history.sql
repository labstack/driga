-- +goose Up
-- Short-lived MCP history is intentionally discarded, not converted.
-- The writer enables incremental vacuum after this atomic migration commits.
DROP TABLE mcp_minutes;
DELETE FROM meta WHERE key='machine:mcp_minute';
CREATE TABLE mcp_groups (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    host_pid INTEGER NOT NULL,
    host_start INTEGER NOT NULL,
    command_key TEXT NOT NULL,
    current_at INTEGER NOT NULL,
    UNIQUE(machine_id,host_pid,host_start,command_key)
);
CREATE TABLE mcp_labels (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    host_name TEXT NOT NULL,
    host_exe TEXT NOT NULL,
    host_cwd TEXT NOT NULL,
    name TEXT NOT NULL,
    UNIQUE(machine_id,host_name,host_exe,host_cwd,name)
);
CREATE TABLE mcp_states (
    machine_id TEXT NOT NULL,
    first_at INTEGER NOT NULL,
    group_id INTEGER NOT NULL REFERENCES mcp_groups(id) ON DELETE CASCADE,
    labels_id INTEGER NOT NULL REFERENCES mcp_labels(id),
    last_at INTEGER NOT NULL,
    count INTEGER NOT NULL,
    footprint INTEGER NOT NULL,
    members TEXT NOT NULL,
    resolution INTEGER NOT NULL DEFAULT 60,
    PRIMARY KEY(machine_id,first_at,group_id)
) WITHOUT ROWID;
CREATE INDEX mcp_states_group ON mcp_states(group_id,first_at);
CREATE INDEX mcp_states_labels ON mcp_states(labels_id);
-- One timestamp per observation, shared by every group. This preserves gaps and
-- exact sample times when a read window cuts through an unchanged state run.
CREATE TABLE mcp_clock (
    machine_id TEXT NOT NULL,
    at INTEGER NOT NULL,
    PRIMARY KEY(machine_id,at)
) WITHOUT ROWID;

-- +goose Down
-- Irreversible: discarded MCP trend history cannot be restored.
SELECT 1;
