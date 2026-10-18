-- +goose Up
CREATE TABLE meta (
    machine_id TEXT NOT NULL,
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE machines (
    id TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL UNIQUE,
    hostname TEXT NOT NULL, os TEXT NOT NULL, arch TEXT NOT NULL,
    cores INTEGER NOT NULL, mem_total INTEGER NOT NULL,
    first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL
);
CREATE TABLE repos (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL, remote_url TEXT NOT NULL,
    root_group TEXT NOT NULL DEFAULT ''
);
CREATE TABLE worktrees (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    repo_id INTEGER NOT NULL REFERENCES repos(id),
    path TEXT NOT NULL UNIQUE,
    branch TEXT NOT NULL, head TEXT NOT NULL,
    ahead INTEGER NOT NULL, behind INTEGER NOT NULL,
    dirty INTEGER NOT NULL, untracked INTEGER NOT NULL,
    oldest_dirty_mtime INTEGER NOT NULL, last_commit_at INTEGER NOT NULL,
    last_commit_subject TEXT NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    adapter TEXT NOT NULL, external_id TEXT NOT NULL,
    parent_session_id INTEGER REFERENCES sessions(id) ON DELETE SET NULL,
    origin TEXT NOT NULL, version TEXT NOT NULL,
    pid INTEGER NOT NULL, proc_start INTEGER NOT NULL,
    cwd TEXT NOT NULL, branch TEXT NOT NULL,
    repo_id INTEGER REFERENCES repos(id),
    worktree_id INTEGER REFERENCES worktrees(id),
    kind TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL, last_prompt TEXT NOT NULL, registry_status TEXT NOT NULL,
    state TEXT NOT NULL, state_heuristic INTEGER NOT NULL, evidence TEXT NOT NULL,
    started_at INTEGER NOT NULL, last_activity_at INTEGER NOT NULL, last_prompt_at INTEGER NOT NULL DEFAULT 0,
    hosted INTEGER NOT NULL, turn_open INTEGER NOT NULL,
    ended_at INTEGER, seen_at INTEGER NOT NULL DEFAULT 0, source_path TEXT NOT NULL
);
CREATE UNIQUE INDEX sessions_identity ON sessions(machine_id, adapter, external_id);
CREATE INDEX sessions_repo ON sessions(machine_id, repo_id);
CREATE TABLE session_files (
    machine_id TEXT NOT NULL,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    path TEXT NOT NULL, repo_rel_path TEXT NOT NULL DEFAULT '',
    last_edit_at INTEGER NOT NULL,
    PRIMARY KEY(machine_id, session_id, path)
);
CREATE INDEX session_files_last_edit ON session_files(last_edit_at);
CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    adapter TEXT NOT NULL, external_id TEXT NOT NULL,
    ts INTEGER NOT NULL, kind TEXT NOT NULL, summary TEXT NOT NULL
);
CREATE INDEX events_session_ts ON events(machine_id, adapter, external_id, ts);
CREATE TABLE handoffs (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    ts INTEGER NOT NULL, text TEXT NOT NULL
);
CREATE INDEX handoffs_session_ts ON handoffs(machine_id, session_id, ts);
CREATE TABLE procs (
    machine_id TEXT NOT NULL,
    pid INTEGER NOT NULL, ppid INTEGER NOT NULL, start_time INTEGER NOT NULL,
    rss INTEGER NOT NULL, cpu_seconds REAL NOT NULL, cpu_percent REAL NOT NULL,
    cmdline TEXT NOT NULL, exe TEXT NOT NULL, role TEXT NOT NULL, agent TEXT NOT NULL,
    session_id INTEGER NOT NULL, cwd TEXT NOT NULL, seen_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(machine_id, pid)
);
CREATE TABLE orphans (
    machine_id TEXT NOT NULL,
    id TEXT NOT NULL, root_pid INTEGER NOT NULL, start_time INTEGER NOT NULL,
    signature TEXT NOT NULL, proc_count INTEGER NOT NULL, rss_total INTEGER NOT NULL,
    cwd TEXT NOT NULL, young INTEGER NOT NULL, evidence TEXT NOT NULL, pids TEXT NOT NULL,
    first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(machine_id, id)
);
CREATE TABLE machine_samples (
    machine_id TEXT NOT NULL, ts INTEGER NOT NULL,
    mem_used INTEGER NOT NULL, mem_compressed INTEGER NOT NULL, mem_total INTEGER NOT NULL,
    swap_used INTEGER NOT NULL, swap_total INTEGER NOT NULL, pressure_free REAL NOT NULL,
    load1 REAL NOT NULL, load5 REAL NOT NULL, load15 REAL NOT NULL,
    disk_free INTEGER NOT NULL, cores INTEGER NOT NULL,
    PRIMARY KEY(machine_id, ts)
);
CREATE INDEX machine_samples_ts ON machine_samples(ts);
CREATE TABLE offsets (
    machine_id TEXT NOT NULL, path TEXT NOT NULL,
    inode INTEGER NOT NULL, size INTEGER NOT NULL, offset INTEGER NOT NULL, mtime INTEGER NOT NULL,
    PRIMARY KEY(machine_id, path)
);
CREATE TABLE inbox_items (
    machine_id TEXT NOT NULL, id TEXT PRIMARY KEY,
    session_id INTEGER NOT NULL, kind TEXT NOT NULL, ts INTEGER NOT NULL,
    summary TEXT NOT NULL, acked_at INTEGER
);
CREATE INDEX inbox_items_ts ON inbox_items(machine_id, ts);
CREATE TABLE sources (
    machine_id TEXT NOT NULL, name TEXT NOT NULL,
    last_ok_at INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
    last_error_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(machine_id, name)
);
CREATE TABLE gaps (
    machine_id TEXT NOT NULL, from_ts INTEGER NOT NULL, to_ts INTEGER NOT NULL,
    PRIMARY KEY(machine_id, from_ts, to_ts)
);

-- +goose Down
DROP TABLE gaps;
DROP TABLE sources;
DROP TABLE inbox_items;
DROP TABLE offsets;
DROP TABLE machine_samples;
DROP TABLE orphans;
DROP TABLE procs;
DROP TABLE handoffs;
DROP TABLE events;
DROP TABLE session_files;
DROP TABLE sessions;
DROP TABLE worktrees;
DROP TABLE repos;
DROP TABLE machines;
DROP TABLE meta;
