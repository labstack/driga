-- +goose Up
ALTER TABLE machines ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN addr TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN driga_version TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN feed_epoch TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN feed_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE machines ADD COLUMN last_contact_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE machines ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN clock_skew_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE machines ADD COLUMN state TEXT NOT NULL DEFAULT 'local';
UPDATE machines SET name = hostname;

-- Open disables foreign keys for migrations; Goose keeps this entire rebuild
-- in one transaction. Preserve IDs so dependent rows retain their references.
CREATE TABLE repos_fleet (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    path TEXT NOT NULL,
    name TEXT NOT NULL, remote_url TEXT NOT NULL,
    root_group TEXT NOT NULL DEFAULT ''
);
INSERT INTO repos_fleet SELECT * FROM repos;
DROP TABLE repos;
ALTER TABLE repos_fleet RENAME TO repos;
CREATE UNIQUE INDEX repos_identity ON repos(machine_id, path);

CREATE TABLE worktrees_fleet (
    id INTEGER PRIMARY KEY,
    machine_id TEXT NOT NULL,
    repo_id INTEGER NOT NULL REFERENCES repos(id),
    path TEXT NOT NULL,
    branch TEXT NOT NULL, head TEXT NOT NULL,
    ahead INTEGER NOT NULL, behind INTEGER NOT NULL,
    dirty INTEGER NOT NULL, untracked INTEGER NOT NULL,
    oldest_dirty_mtime INTEGER NOT NULL, last_commit_at INTEGER NOT NULL,
    last_commit_subject TEXT NOT NULL, updated_at INTEGER NOT NULL
);
INSERT INTO worktrees_fleet SELECT * FROM worktrees;
DROP TABLE worktrees;
ALTER TABLE worktrees_fleet RENAME TO worktrees;
CREATE UNIQUE INDEX worktrees_identity ON worktrees(machine_id, path);

CREATE TABLE inbox_items_fleet (
    machine_id TEXT NOT NULL, id TEXT NOT NULL,
    session_id INTEGER NOT NULL, kind TEXT NOT NULL, ts INTEGER NOT NULL,
    summary TEXT NOT NULL, acked_at INTEGER, ack_pending_at INTEGER,
    PRIMARY KEY(machine_id, id)
);
INSERT INTO inbox_items_fleet (machine_id,id,session_id,kind,ts,summary,acked_at)
    SELECT machine_id,id,session_id,kind,ts,summary,acked_at FROM inbox_items;
DROP TABLE inbox_items;
ALTER TABLE inbox_items_fleet RENAME TO inbox_items;
CREATE INDEX inbox_items_ts ON inbox_items(machine_id, ts);

CREATE TABLE feed (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    ts INTEGER NOT NULL, kind TEXT NOT NULL, key TEXT NOT NULL, payload TEXT NOT NULL
);
CREATE INDEX feed_ts ON feed(ts);
ALTER TABLE machine_samples ADD COLUMN psi_mem_some10 REAL;

-- +goose Down
-- Reverting per-machine uniqueness would discard colliding remote rows.
CREATE TEMP TABLE fleet_downgrade_guard (id INTEGER);
-- +goose StatementBegin
CREATE TEMP TRIGGER fleet_downgrade_refused BEFORE INSERT ON fleet_downgrade_guard
BEGIN
    SELECT RAISE(ABORT, 'fleet migration cannot be downgraded');
END;
-- +goose StatementEnd
INSERT INTO fleet_downgrade_guard VALUES (1);
