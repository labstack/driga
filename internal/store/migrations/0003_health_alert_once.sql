-- +goose Up
DROP INDEX inbox_alert_candidates;
CREATE INDEX inbox_alert_candidates ON inbox_items(machine_id, ts)
    WHERE acked_at IS NULL AND ended_at IS NULL AND alerted=0;

-- +goose Down
DROP INDEX inbox_alert_candidates;
CREATE INDEX inbox_alert_candidates ON inbox_items(machine_id, ts)
    WHERE acked_at IS NULL AND ended_at IS NULL
      AND (alerted=0 OR kind IN ('low_disk','memory_pressure'));
