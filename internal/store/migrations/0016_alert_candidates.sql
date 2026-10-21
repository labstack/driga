-- +goose Up
-- Viewer-local projection of the existing durable seen metadata. Never shared
-- in the fleet feed; authoritative peer updates preserve this local column.
ALTER TABLE inbox_items ADD COLUMN alerted INTEGER NOT NULL DEFAULT 0;
-- Existing rows are hydrated against the original metadata on their first
-- alert pass, which also preserves handling of malformed/empty metadata.
CREATE INDEX inbox_alert_candidates ON inbox_items(machine_id,ts)
WHERE acked_at IS NULL AND ended_at IS NULL
AND (alerted=0 OR kind IN ('low_disk','memory_pressure'));

-- +goose Down
DROP INDEX inbox_alert_candidates;
ALTER TABLE inbox_items DROP COLUMN alerted;
