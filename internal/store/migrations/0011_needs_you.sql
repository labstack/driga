-- +goose Up
ALTER TABLE inbox_items ADD COLUMN reason TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox_items ADD COLUMN tool TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox_items ADD COLUMN detail TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox_items ADD COLUMN waiting_since_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inbox_items ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox_items ADD COLUMN hook_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox_items ADD COLUMN agents_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox_items ADD COLUMN ended_at INTEGER;
ALTER TABLE inbox_items ADD COLUMN jump TEXT NOT NULL DEFAULT 'null';
ALTER TABLE procs ADD COLUMN tty TEXT NOT NULL DEFAULT '';
UPDATE inbox_items SET reason='unknown',source='hook',hook_reason='unknown',waiting_since_at=ts WHERE kind='waiting';
-- Old waits were grouped onto the lead. Recover the original session from
-- their existing natural key, without changing completion grouping or acks.
UPDATE inbox_items SET session_id=COALESCE((
    SELECT s.id FROM sessions s WHERE s.machine_id=inbox_items.machine_id
    AND substr(inbox_items.id,1,length(s.adapter||':'||s.external_id||':'))=s.adapter||':'||s.external_id||':'
    ORDER BY length(s.external_id) DESC LIMIT 1
),session_id) WHERE kind='waiting';
-- Preserve legacy repeated notifications as ended history; keep the earliest
-- unacknowledged row as the single current wait. Remote rows remain authoritative.
INSERT INTO meta(machine_id,key,value)
SELECT i.machine_id,'inbox_alias:'||i.id,(
    SELECT first.id FROM inbox_items first WHERE first.machine_id=i.machine_id
    AND first.session_id=i.session_id AND first.kind='waiting' AND first.acked_at IS NULL
    ORDER BY first.ts,first.id LIMIT 1
) FROM inbox_items i WHERE i.kind='waiting' AND i.acked_at IS NULL
AND i.machine_id=(SELECT value FROM meta WHERE key='machine_id');
UPDATE inbox_items SET ended_at=ts WHERE kind='waiting' AND acked_at IS NULL
AND machine_id=(SELECT value FROM meta WHERE key='machine_id')
AND id<>(SELECT value FROM meta WHERE key='inbox_alias:'||inbox_items.id);
CREATE INDEX inbox_waits ON inbox_items(machine_id,session_id,kind,acked_at,ended_at);

-- +goose Down
DELETE FROM meta WHERE key LIKE 'inbox_alias:%';
DROP INDEX inbox_waits;
ALTER TABLE procs DROP COLUMN tty;
ALTER TABLE inbox_items DROP COLUMN jump;
ALTER TABLE inbox_items DROP COLUMN ended_at;
ALTER TABLE inbox_items DROP COLUMN source;
ALTER TABLE inbox_items DROP COLUMN agents_reason;
ALTER TABLE inbox_items DROP COLUMN hook_reason;
ALTER TABLE inbox_items DROP COLUMN waiting_since_at;
ALTER TABLE inbox_items DROP COLUMN detail;
ALTER TABLE inbox_items DROP COLUMN tool;
ALTER TABLE inbox_items DROP COLUMN reason;
