package store

import "time"

// EventCount is a machine-local minute's event total across sessions.
type EventCount struct {
	TS    time.Time
	Count int
}

// EventCounts includes both bounds and returns only minutes containing events.
func (q *querier) EventCounts(since, until time.Time) ([]EventCount, error) {
	return readMany(q, `SELECT minute,count
 - (SELECT COUNT(*) FROM events e WHERE e.machine_id=m.machine_id AND e.ts> ? AND e.ts>=m.minute AND e.ts<m.minute+60000000000)
 - (SELECT COUNT(*) FROM events e WHERE e.machine_id=m.machine_id AND e.ts< ? AND e.ts>=m.minute AND e.ts<m.minute+60000000000)
 FROM event_minutes m
 WHERE machine_id=? AND minute>=? AND minute<=? ORDER BY minute`,
		[]any{unix(until), unix(since), q.machineID, unix(since.Truncate(time.Minute)), unix(until.Truncate(time.Minute))}, func(s scanner) (EventCount, error) {
			var c EventCount
			var ts int64
			err := s.Scan(&ts, &c.Count)
			c.TS = fromUnix(ts)
			return c, err
		})
}
