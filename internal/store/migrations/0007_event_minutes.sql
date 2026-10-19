-- +goose Up
CREATE TABLE event_minutes (
 machine_id TEXT NOT NULL,
 minute INTEGER NOT NULL,
 count INTEGER NOT NULL,
 PRIMARY KEY(machine_id, minute)
);
INSERT INTO event_minutes(machine_id,minute,count)
SELECT machine_id,(ts/60000000000)*60000000000,COUNT(*) FROM events GROUP BY machine_id,ts/60000000000;

-- +goose Down
DROP TABLE event_minutes;
