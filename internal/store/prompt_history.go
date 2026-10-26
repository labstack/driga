package store

import (
	"context"
	"database/sql"
	"time"
)

// PromptInterval contains no prompt/session/tool/input text.
type PromptInterval struct {
	StartedAt time.Time
	EndedAt   *time.Time
}

func (s *Store) StartPromptHistory(ctx context.Context, id string, at time.Time) error {
	return s.Tx(ctx, func(tx *Tx) error {
		return tx.exec("INSERT INTO prompt_intervals(machine_id,id,started_at) VALUES(?,?,?)", tx.machineID, id, unix(at))
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
	return readMany(q, "SELECT started_at,ended_at FROM prompt_intervals WHERE machine_id=? AND (ended_at IS NULL OR ended_at>=?)", []any{q.machineID, unix(since)}, func(s scanner) (PromptInterval, error) {
		var p PromptInterval
		var start int64
		var end sql.NullInt64
		err := s.Scan(&start, &end)
		p.StartedAt = fromUnix(start)
		p.EndedAt = timePointer(end)
		return p, err
	})
}
