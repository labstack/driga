package store

import "time"

// ObservationMinute records positive collector measurements, never transcript content.
// Samples counts passes with positive bytes or CPU, rather than idle polls.
type ObservationMinute struct {
	Minute          time.Time
	LogBytes, CPUMS int64
	Samples         int
	LastGrowthAt    time.Time
	LastGrowthBytes int64
}

// Observe participates in the pass transaction. Idle measurements execute no SQL.
func (tx *Tx) Observe(sessionID int64, at time.Time, logBytes, cpuMS int64) error {
	logBytes, cpuMS = max(0, logBytes), max(0, cpuMS)
	if logBytes == 0 && cpuMS == 0 {
		return nil
	}
	growthAt := int64(0)
	if logBytes > 0 {
		growthAt = at.UnixMilli()
	}
	return tx.exec(`INSERT INTO observation_minutes(machine_id,session_id,minute,log_bytes,cpu_ms,samples,last_growth_at,last_growth_bytes)
 VALUES(?,?,?,?,?,1,?,?) ON CONFLICT(machine_id,session_id,minute) DO UPDATE SET
 log_bytes=log_bytes+excluded.log_bytes,cpu_ms=cpu_ms+excluded.cpu_ms,samples=samples+1,
 last_growth_bytes=CASE WHEN excluded.last_growth_at>0 AND excluded.last_growth_at>=last_growth_at THEN excluded.last_growth_bytes ELSE last_growth_bytes END,
 last_growth_at=MAX(last_growth_at,excluded.last_growth_at)`, tx.machineID, sessionID, at.Truncate(time.Minute).UnixMilli(), logBytes, cpuMS, growthAt, logBytes)
}

// ObservationMinutes returns retained buckets through the requested minute,
// oldest first. Older buckets preserve last-growth provenance outside the chart.
func (q *querier) ObservationMinutes(sessionID int64, until time.Time) ([]ObservationMinute, error) {
	return readMany(q, `SELECT minute,log_bytes,cpu_ms,samples,last_growth_at,last_growth_bytes FROM observation_minutes
 WHERE machine_id=? AND session_id=? AND minute<=? ORDER BY minute`, []any{q.machineID, sessionID, until.Truncate(time.Minute).UnixMilli()}, func(s scanner) (ObservationMinute, error) {
		var r ObservationMinute
		var minute, at int64
		err := s.Scan(&minute, &r.LogBytes, &r.CPUMS, &r.Samples, &at, &r.LastGrowthBytes)
		r.Minute = time.UnixMilli(minute).UTC()
		if at != 0 {
			r.LastGrowthAt = time.UnixMilli(at).UTC()
		}
		return r, err
	})
}
