package store

import "time"

// EventCount is a machine-local minute's event total across sessions.
type EventCount struct {
	TS    time.Time
	Count int
}

// EventCounts includes both bounds and returns only minutes containing events.
func (q *querier) EventCounts(since, until time.Time) ([]EventCount, error) {
	const minute = int64(time.Minute)
	return readMany(q, `SELECT (ts / ?) * ?, COUNT(*) FROM events
		WHERE machine_id=? AND ts>=? AND ts<=? GROUP BY ts / ? ORDER BY ts / ?`,
		[]any{minute, minute, q.machineID, unix(since), unix(until), minute, minute}, func(s scanner) (EventCount, error) {
			var c EventCount
			var ts int64
			err := s.Scan(&ts, &c.Count)
			c.TS = fromUnix(ts)
			return c, err
		})
}
