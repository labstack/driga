-- +goose Up
-- Activity counts events per minute for a machine on every dashboard refresh.
CREATE INDEX events_machine_ts ON events(machine_id, ts);

-- +goose Down
DROP INDEX events_machine_ts;
