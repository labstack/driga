-- +goose Up
-- Fresh databases only. Pre-release databases must be reset.
-- Distinguish this baseline from the former version-1 migration.
PRAGMA user_version = 1;

-- Machine identity and collector metadata.
CREATE TABLE meta (
    machine_id TEXT NOT NULL,
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE machines (
    id TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL UNIQUE,
    hostname TEXT NOT NULL,
    os TEXT NOT NULL,
    arch TEXT NOT NULL,
    cores INTEGER NOT NULL,
    mem_total INTEGER NOT NULL,
    first_seen INTEGER NOT NULL,
    last_seen INTEGER NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    device_id TEXT NOT NULL DEFAULT '',
    addr TEXT NOT NULL DEFAULT '',
    driga_version TEXT NOT NULL DEFAULT '',
    feed_epoch TEXT NOT NULL DEFAULT '',
    feed_seq INTEGER NOT NULL DEFAULT 0,
    last_contact_at INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    clock_skew_ms INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'local'
);

CREATE TABLE sources (
    machine_id TEXT NOT NULL,
    name TEXT NOT NULL,
    last_ok_at INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    last_error_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(machine_id, name)
);

CREATE TABLE gaps (
    machine_id TEXT NOT NULL,
    from_ts INTEGER NOT NULL,
    to_ts INTEGER NOT NULL,
    PRIMARY KEY(machine_id, from_ts, to_ts)
);

CREATE TABLE offsets (
    machine_id TEXT NOT NULL,
    path TEXT NOT NULL,
    inode INTEGER NOT NULL,
    size INTEGER NOT NULL,
    offset INTEGER NOT NULL,
    mtime INTEGER NOT NULL,
    PRIMARY KEY(machine_id, path)
);

-- Repositories and observed sessions.
CREATE TABLE repos (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    path TEXT NOT NULL,
    name TEXT NOT NULL,
    remote_url TEXT NOT NULL,
    root_group TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX repos_identity ON repos(machine_id, path);

CREATE TABLE worktrees (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    repo_id INTEGER NOT NULL REFERENCES repos(id),
    path TEXT NOT NULL,
    branch TEXT NOT NULL,
    head TEXT NOT NULL,
    ahead INTEGER NOT NULL,
    behind INTEGER NOT NULL,
    dirty INTEGER NOT NULL,
    untracked INTEGER NOT NULL,
    oldest_dirty_mtime INTEGER NOT NULL,
    last_commit_at INTEGER NOT NULL,
    last_commit_subject TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX worktrees_identity ON worktrees(machine_id, path);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    adapter TEXT NOT NULL,
    external_id TEXT NOT NULL,
    parent_session_id INTEGER REFERENCES sessions(id) ON DELETE SET NULL,
    origin TEXT NOT NULL,
    version TEXT NOT NULL,
    pid INTEGER NOT NULL,
    proc_start INTEGER NOT NULL,
    cwd TEXT NOT NULL,
    branch TEXT NOT NULL,
    repo_id INTEGER REFERENCES repos(id),
    worktree_id INTEGER REFERENCES worktrees(id),
    kind TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    last_prompt TEXT NOT NULL,
    registry_status TEXT NOT NULL,
    state TEXT NOT NULL,
    state_heuristic INTEGER NOT NULL,
    evidence TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    last_activity_at INTEGER NOT NULL,
    last_prompt_at INTEGER NOT NULL DEFAULT 0,
    hosted INTEGER NOT NULL,
    turn_open INTEGER NOT NULL,
    ended_at INTEGER,
    seen_at INTEGER NOT NULL DEFAULT 0,
    source_path TEXT NOT NULL,
    last_decision_at INTEGER NOT NULL DEFAULT 0,
    last_api_request_at INTEGER NOT NULL DEFAULT 0,
    last_api_error TEXT NOT NULL DEFAULT '',
    mixer TEXT NOT NULL DEFAULT 'null',
    started_from TEXT,
    registry_updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX sessions_identity ON sessions(machine_id, adapter, external_id);
CREATE INDEX sessions_repo ON sessions(machine_id, repo_id);

CREATE TABLE session_files (
    machine_id TEXT NOT NULL,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    repo_rel_path TEXT NOT NULL DEFAULT '',
    last_edit_at INTEGER NOT NULL,
    PRIMARY KEY(machine_id, session_id, path)
);
CREATE INDEX session_files_last_edit ON session_files(last_edit_at);

CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    adapter TEXT NOT NULL,
    external_id TEXT NOT NULL,
    ts INTEGER NOT NULL,
    kind TEXT NOT NULL,
    summary TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT 'null'
);
CREATE INDEX events_machine_ts ON events(machine_id, ts);
CREATE INDEX events_session_ts ON events(machine_id, adapter, external_id, ts);

CREATE TABLE handoffs (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    ts INTEGER NOT NULL,
    text TEXT NOT NULL
);
CREATE INDEX handoffs_session_ts ON handoffs(machine_id, session_id, ts);

CREATE TABLE inbox_items (
    machine_id TEXT NOT NULL,
    id TEXT NOT NULL,
    session_id INTEGER NOT NULL,
    kind TEXT NOT NULL,
    ts INTEGER NOT NULL,
    summary TEXT NOT NULL,
    acked_at INTEGER,
    ack_pending_at INTEGER,
    reason TEXT NOT NULL DEFAULT '',
    tool TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    waiting_since_at INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT '',
    hook_reason TEXT NOT NULL DEFAULT '',
    agents_reason TEXT NOT NULL DEFAULT '',
    ended_at INTEGER,
    jump TEXT NOT NULL DEFAULT 'null',
    alerted INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(machine_id, id)
);
CREATE INDEX inbox_alert_candidates ON inbox_items(machine_id, ts)
    WHERE acked_at IS NULL AND ended_at IS NULL
      AND (alerted = 0 OR kind IN ('low_disk', 'memory_pressure'));
CREATE INDEX inbox_items_ts ON inbox_items(machine_id, ts);
CREATE INDEX inbox_waits ON inbox_items(machine_id, session_id, kind, acked_at, ended_at);

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

-- Process snapshots and machine health.
CREATE TABLE procs (
    machine_id TEXT NOT NULL,
    pid INTEGER NOT NULL,
    ppid INTEGER NOT NULL,
    start_time INTEGER NOT NULL,
    rss INTEGER NOT NULL,
    cpu_seconds REAL NOT NULL,
    cpu_percent REAL NOT NULL,
    cmdline TEXT NOT NULL,
    exe TEXT NOT NULL,
    role TEXT NOT NULL,
    agent TEXT NOT NULL,
    session_id INTEGER NOT NULL,
    cwd TEXT NOT NULL,
    seen_at INTEGER NOT NULL DEFAULT 0,
    tty TEXT NOT NULL DEFAULT '',
    footprint INTEGER NOT NULL DEFAULT 0,
    swap INTEGER NOT NULL DEFAULT 0,
    memory_measure TEXT NOT NULL DEFAULT 'rss',
    partial INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(machine_id, pid)
);

CREATE TABLE orphans (
    machine_id TEXT NOT NULL,
    id TEXT NOT NULL,
    root_pid INTEGER NOT NULL,
    start_time INTEGER NOT NULL,
    signature TEXT NOT NULL,
    proc_count INTEGER NOT NULL,
    rss_total INTEGER NOT NULL,
    cwd TEXT NOT NULL,
    young INTEGER NOT NULL,
    evidence TEXT NOT NULL,
    pids TEXT NOT NULL,
    first_seen INTEGER NOT NULL,
    last_seen INTEGER NOT NULL DEFAULT 0,
    verified INTEGER NOT NULL DEFAULT 0,
    process_starts TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY(machine_id, id)
);

CREATE TABLE machine_samples (
    machine_id TEXT NOT NULL,
    ts INTEGER NOT NULL,
    mem_used INTEGER NOT NULL,
    mem_compressed INTEGER NOT NULL,
    mem_total INTEGER NOT NULL,
    swap_used INTEGER NOT NULL,
    swap_total INTEGER NOT NULL,
    pressure_free REAL NOT NULL,
    load1 REAL NOT NULL,
    load5 REAL NOT NULL,
    load15 REAL NOT NULL,
    disk_free INTEGER NOT NULL,
    cores INTEGER NOT NULL,
    psi_mem_some10 REAL,
    pressure_level TEXT NOT NULL DEFAULT 'unknown',
    commit_used INTEGER NOT NULL DEFAULT 0,
    commit_limit INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(machine_id, ts)
);
CREATE INDEX machine_samples_ts ON machine_samples(ts);

-- Activity counts and bounded MCP history.
CREATE TABLE event_minutes (
    machine_id TEXT NOT NULL,
    minute INTEGER NOT NULL,
    count INTEGER NOT NULL,
    PRIMARY KEY(machine_id, minute)
);

CREATE TABLE observation_minutes (
    machine_id TEXT NOT NULL,
    session_id INTEGER NOT NULL,
    minute INTEGER NOT NULL,
    log_bytes INTEGER NOT NULL DEFAULT 0,
    cpu_ms INTEGER NOT NULL DEFAULT 0,
    samples INTEGER NOT NULL DEFAULT 0,
    last_growth_at INTEGER NOT NULL DEFAULT 0,
    last_growth_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (machine_id, session_id, minute)
) WITHOUT ROWID;
CREATE INDEX observation_minutes_session_minute ON observation_minutes(session_id, minute);

CREATE TABLE mcp_groups (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    host_pid INTEGER NOT NULL,
    host_start INTEGER NOT NULL,
    command_key TEXT NOT NULL,
    current_at INTEGER NOT NULL,
    UNIQUE(machine_id, host_pid, host_start, command_key)
);

CREATE TABLE mcp_labels (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    host_name TEXT NOT NULL,
    host_exe TEXT NOT NULL,
    host_cwd TEXT NOT NULL,
    name TEXT NOT NULL,
    UNIQUE(machine_id, host_name, host_exe, host_cwd, name)
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
    PRIMARY KEY(machine_id, first_at, group_id)
) WITHOUT ROWID;
CREATE INDEX mcp_states_group ON mcp_states(group_id, first_at);
CREATE INDEX mcp_states_labels ON mcp_states(labels_id);

-- Observation times preserve gaps and endpoints across unchanged states.
CREATE TABLE mcp_clock (
    machine_id TEXT NOT NULL,
    at INTEGER NOT NULL,
    PRIMARY KEY(machine_id, at)
) WITHOUT ROWID;

-- Fleet changes and local conversations.
CREATE TABLE feed (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    ts INTEGER NOT NULL,
    kind TEXT NOT NULL,
    key TEXT NOT NULL,
    payload TEXT NOT NULL
);
CREATE INDEX feed_ts ON feed(ts);

CREATE TABLE conversations (
    id TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL,
    agent TEXT NOT NULL DEFAULT 'claude',
    session_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX conversations_updated ON conversations(machine_id, updated_at);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX messages_conversation ON messages(conversation_id, created_at);
