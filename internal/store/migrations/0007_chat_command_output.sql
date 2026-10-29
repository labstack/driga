-- +goose Up
CREATE TABLE chat_command_output (
    action_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    stream TEXT NOT NULL CHECK(stream IN ('stdout','stderr')),
    seq INTEGER NOT NULL CHECK(seq >= 0),
    data BLOB NOT NULL,
    PRIMARY KEY(action_id, seq)
);

-- +goose Down
DROP TABLE chat_command_output;
