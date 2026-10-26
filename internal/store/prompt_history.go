package store

import (
	"context"
	"database/sql"
	"time"
)

// PromptInterval contains only a numeric session identity and timestamps.
type PromptInterval struct {
	SessionID int64
	StartedAt time.Time
	EndedAt   *time.Time
}

func (s *Store) StartPromptHistory(ctx context.Context, id string, sessionID int64, at time.Time) error {
	return s.Tx(ctx, func(tx *Tx) error {
		return tx.exec("INSERT INTO prompt_intervals(machine_id,id,session_id,started_at) VALUES(?,?,?,?)", tx.machineID, id, sessionID, unix(at))
	})
}
func (s *Store) EndPromptHistory(ctx context.Context, id string, at time.Time) error {
	return s.Tx(ctx, func(tx *Tx) error {
		return tx.exec("UPDATE prompt_intervals SET ended_at=? WHERE machine_id=? AND id=?", unix(at), tx.machineID, id)
	})
}
func (s *Store) InterruptPromptHistory(ctx context.Context, at time.Time) error {
	return s.Tx(ctx, func(tx *Tx) error {
		if err := tx.exec("DELETE FROM prompt_intervals WHERE machine_id=? AND ended_at<?", tx.machineID, unix(at.Add(-time.Hour))); err != nil {
			return err
		}
		return tx.exec("UPDATE prompt_intervals SET ended_at=? WHERE machine_id=? AND ended_at IS NULL", unix(at), tx.machineID)
	})
}
func (q *querier) PromptIntervals(since time.Time) ([]PromptInterval, error) {
	return readMany(q, "SELECT session_id,started_at,ended_at FROM prompt_intervals WHERE machine_id=? AND (ended_at IS NULL OR ended_at>=?)", []any{q.machineID, unix(since)}, func(s scanner) (PromptInterval, error) {
		var p PromptInterval
		var start int64
		var end sql.NullInt64
		err := s.Scan(&p.SessionID, &start, &end)
		p.StartedAt = fromUnix(start)
		p.EndedAt = timePointer(end)
		return p, err
	})
}

// LinkPromptHistory records a session discovered after prompt registration.
// The timestamp binds the update to this exact registration, even if an ID is reused.
func (s *Store) LinkPromptHistory(ctx context.Context, id string, startedAt time.Time, sessionID int64) error {
	return s.Tx(ctx, func(tx *Tx) error {
		return tx.exec("UPDATE prompt_intervals SET session_id=? WHERE machine_id=? AND id=? AND started_at=?", sessionID, tx.machineID, id, unix(startedAt))
	})
}
