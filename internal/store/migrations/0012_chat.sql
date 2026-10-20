-- +goose Up
CREATE TABLE conversations (
 id TEXT PRIMARY KEY,
 machine_id TEXT NOT NULL,
 agent TEXT NOT NULL DEFAULT 'claude',
 session_id TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX conversations_updated ON conversations(machine_id,updated_at);
CREATE TABLE messages (
 id TEXT PRIMARY KEY,
 machine_id TEXT NOT NULL,
 conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 role TEXT NOT NULL,
 text TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL,
 created_at INTEGER NOT NULL
);
CREATE INDEX messages_conversation ON messages(conversation_id,created_at);
-- +goose Down
DROP TABLE messages;
DROP TABLE conversations;
