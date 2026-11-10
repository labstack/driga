package store

import (
	"slices"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

// UnseenFinishedSessions returns only completion presence, not inbox payloads.
// Nil IDs selects all local sessions; an empty nonnil slice selects none.
func (q *querier) UnseenFinishedSessions(ids []int64) (map[int64]bool, error) {
	result := map[int64]bool{}
	if ids != nil && len(ids) == 0 {
		return result, nil
	}
	err := labelIDBatches(ids, func(batch []int64) error { return q.unseenFinishedBatch(batch, result) })
	return result, err
}

func (q *querier) unseenFinishedBatch(ids []int64, result map[int64]bool) error {
	query := "SELECT DISTINCT session_id FROM inbox_items WHERE machine_id=? AND ts>=? AND kind='finished' AND acked_at IS NULL AND ended_at IS NULL"
	args := []any{q.machineID, unix(time.Time{})}
	if ids != nil {
		query += " AND session_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := q.conn.QueryContext(q.ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		result[id] = true
	}
	return rows.Err()
}

// LabelProcs reads the fresh identity/CPU facts needed for state labeling. It
// deliberately omits argv, cwd and other processes' unrelated display data.
func (q *querier) LabelProcs(pids []int32) ([]model.Proc, error) {
	if len(pids) == 0 {
		return []model.Proc{}, nil
	}
	result := []model.Proc{}
	err := labelIDBatches(pids, func(batch []int32) error {
		procs, err := q.labelProcBatch(batch)
		if err == nil {
			result = append(result, procs...)
		}
		return err
	})
	return result, err
}

func (q *querier) labelProcBatch(pids []int32) ([]model.Proc, error) {
	args := []any{q.machineID}
	for _, pid := range pids {
		args = append(args, pid)
	}
	query := "SELECT pid,start_time,cpu_percent FROM procs WHERE machine_id=? AND pid IN (" + strings.TrimSuffix(strings.Repeat("?,", len(pids)), ",") + ") ORDER BY pid"
	return readMany(q, query, args, func(s scanner) (model.Proc, error) {
		var p model.Proc
		var at int64
		err := s.Scan(&p.PID, &at, &p.CPUPercent)
		p.StartTime = fromUnix(at)
		return p, err
	})
}

// Bound SQL parameters and preserve set/order semantics across batches.
func labelIDBatches[T ~int32 | ~int64](ids []T, read func([]T) error) error {
	if ids == nil {
		return read(nil)
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for len(ids) > 0 {
		n := min(512, len(ids))
		if err := read(ids[:n]); err != nil {
			return err
		}
		ids = ids[n:]
	}
	return nil
}
