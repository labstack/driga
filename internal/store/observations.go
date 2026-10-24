package store

import (
	"github.com/labstack/driga/internal/model"
	"time"
)

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

// BoardEvidence loads the selected machine window with two queries, independent
// of session count. Event volume is bounded to the newest 12000 stored events.
func (q *querier) BoardEvidence(since, until time.Time) ([]model.Event, map[int64][]ObservationMinute, error) {
	events, err := readMany(q, `SELECT machine_id,adapter,external_id,ts,kind,summary,metadata FROM events WHERE machine_id=? AND ts>=? AND ts<=? ORDER BY ts DESC,id DESC LIMIT 12000`, []any{q.machineID, unix(since), unix(until)}, scanEvent)
	if err != nil {
		return nil, nil, err
	}
	type entry struct {
		id  int64
		row ObservationMinute
	}
	rows, err := readMany(q, `SELECT session_id,minute,log_bytes,cpu_ms,samples,last_growth_at,last_growth_bytes FROM observation_minutes WHERE machine_id=? AND minute>=? AND minute<=? ORDER BY minute`, []any{q.machineID, since.Truncate(time.Minute).UnixMilli(), until.Truncate(time.Minute).UnixMilli()}, func(s scanner) (entry, error) {
		var e entry
		var minute, at int64
		err := s.Scan(&e.id, &minute, &e.row.LogBytes, &e.row.CPUMS, &e.row.Samples, &at, &e.row.LastGrowthBytes)
		e.row.Minute = time.UnixMilli(minute).UTC()
		if at != 0 {
			e.row.LastGrowthAt = time.UnixMilli(at).UTC()
		}
		return e, err
	})
	if err != nil {
		return nil, nil, err
	}
	observed := map[int64][]ObservationMinute{}
	for _, e := range rows {
		observed[e.id] = append(observed[e.id], e.row)
	}
	return events, observed, nil
}
