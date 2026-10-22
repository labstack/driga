package store

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/labstack/driga/internal/model"
)

func (q *querier) MCPObservationDue(at time.Time) (bool, error) {
	raw, err := q.Meta("machine:mcp_observation")
	if err != nil && err != ErrNotFound {
		return false, err
	}
	previous, parseErr := strconv.ParseInt(raw, 10, 64)
	return parseErr != nil || unix(at.Truncate(time.Minute)) > previous, nil
}

type mcpState struct {
	id, first, last int64
	sample          model.MCPObservation
	members         string
}

func scanMCPState(s scanner) (mcpState, error) {
	var r mcpState
	var start int64
	err := s.Scan(&r.id, &r.first, &r.last, &r.sample.Host.PID, &start, &r.sample.Key, &r.sample.Host.Name, &r.sample.Host.Exe, &r.sample.Host.Cwd, &r.sample.Name, &r.sample.Count, &r.sample.Footprint, &r.members, &r.sample.ResolutionSeconds)
	r.sample.Host.StartedAt = fromUnix(start)
	if err == nil {
		err = json.Unmarshal([]byte(r.members), &r.sample.Members)
	}
	return r, err
}

const mcpStateColumns = `g.id,s.first_at,s.last_at,g.host_pid,g.host_start,g.command_key,l.host_name,l.host_exe,l.host_cwd,l.name,s.count,s.footprint,s.members,s.resolution`

// ObserveMCP gates observations to the first successful sample in each minute.
// Unchanged states extend their endpoint in place; an hour boundary starts a
// bounded heartbeat run. Missing groups receive exactly one explicit zero.
func (tx *Tx) ObserveMCP(at time.Time, samples []model.MCPObservation) error {
	due, err := tx.MCPObservationDue(at)
	if err != nil || !due {
		return err
	}
	raw, err := tx.Meta("machine:mcp_observation")
	if err != nil && err != ErrNotFound {
		return err
	}
	previousMinute, _ := strconv.ParseInt(raw, 10, 64)
	previous, err := readMany(&tx.querier, `SELECT `+mcpStateColumns+` FROM mcp_groups g JOIN mcp_states s ON s.machine_id=g.machine_id AND s.first_at=g.current_at AND s.group_id=g.id JOIN mcp_labels l ON l.id=s.labels_id WHERE g.machine_id=? AND s.first_at>=? AND s.first_at<=?`, []any{tx.machineID, unix(fromUnix(previousMinute).Truncate(time.Hour)), unix(at)}, scanMCPState)
	if err != nil {
		return err
	}
	type key struct {
		pid     int32
		start   int64
		command string
	}
	keyOf := func(s model.MCPObservation) key { return key{s.Host.PID, unix(s.Host.StartedAt), s.Key} }
	old := make(map[key]mcpState, len(previous))
	for _, r := range previous {
		old[keyOf(r.sample)] = r
	}
	seen := make(map[key]bool, len(samples))
	write := func(s model.MCPObservation) error {
		k := keyOf(s)
		if seen[k] {
			return nil
		}
		seen[k] = true
		// Membership is a set; process enumeration order must not create changes.
		s.Members = append([]model.ProcessIdentity(nil), s.Members...)
		sort.Slice(s.Members, func(i, j int) bool {
			a, b := s.Members[i], s.Members[j]
			if a.PID != b.PID {
				return a.PID < b.PID
			}
			return a.StartedAt.Before(b.StartedAt)
		})
		members, err := json.Marshal(s.Members)
		if err != nil {
			return err
		}
		prev, exists := old[k]
		if exists && prev.sample.Count == s.Count && prev.sample.Footprint == s.Footprint && prev.members == string(members) && prev.sample.Host.Name == s.Host.Name && prev.sample.Host.Exe == s.Host.Exe && prev.sample.Host.Cwd == s.Host.Cwd && prev.sample.Name == s.Name && fromUnix(prev.first).Truncate(time.Hour).Equal(at.Truncate(time.Hour)) {
			return tx.exec(`UPDATE mcp_states SET last_at=? WHERE machine_id=? AND first_at=? AND group_id=?`, unix(at), tx.machineID, prev.first, prev.id)
		}
		var labelsID int64
		err = tx.conn.QueryRowContext(tx.ctx, `INSERT INTO mcp_labels(machine_id,host_name,host_exe,host_cwd,name) VALUES(?,?,?,?,?) ON CONFLICT(machine_id,host_name,host_exe,host_cwd,name) DO UPDATE SET name=excluded.name RETURNING id`, tx.machineID, s.Host.Name, s.Host.Exe, s.Host.Cwd, s.Name).Scan(&labelsID)
		if err != nil {
			return err
		}
		var id int64
		err = tx.conn.QueryRowContext(tx.ctx, `INSERT INTO mcp_groups(machine_id,host_pid,host_start,command_key,current_at) VALUES(?,?,?,?,?) ON CONFLICT(machine_id,host_pid,host_start,command_key) DO UPDATE SET current_at=excluded.current_at RETURNING id`, tx.machineID, s.Host.PID, unix(s.Host.StartedAt), s.Key, unix(at)).Scan(&id)
		if err != nil {
			return err
		}
		return tx.exec(`INSERT INTO mcp_states(machine_id,first_at,group_id,labels_id,last_at,count,footprint,members) VALUES(?,?,?,?,?,?,?,?)`, tx.machineID, unix(at), id, labelsID, unix(at), s.Count, s.Footprint, string(members))
	}
	for _, s := range samples {
		if err := write(s); err != nil {
			return err
		}
	}
	for _, r := range previous {
		if r.sample.Count == 0 || seen[keyOf(r.sample)] {
			continue
		}
		s := r.sample
		s.Count, s.Footprint, s.Members = 0, 0, nil
		if err := write(s); err != nil {
			return err
		}
	}
	if err := tx.exec(`INSERT INTO mcp_clock(machine_id,at) VALUES(?,?)`, tx.machineID, unix(at)); err != nil {
		return err
	}
	return tx.SetMeta("machine:mcp_observation", strconv.FormatInt(unix(at.Truncate(time.Minute)), 10))
}

// ScanMCPHistory requires a bounded window, capped at the retained 30 days.
// Each run contributes its first and last actual observations in the window.
// Equal intermediate observations cannot change membership transition math.
// The shared clock avoids inventing observations across collector gaps.
// Endpoints are chronological within each group; groups can interleave.
func (q *querier) ScanMCPHistory(since, until time.Time, visit func(model.MCPObservation) error) error {
	since = maxTime(since, until.Add(-30*24*time.Hour)).Truncate(time.Minute)
	rows, err := q.conn.QueryContext(q.ctx, `SELECT `+mcpStateColumns+`,
 CASE WHEN s.resolution=60 THEN (SELECT MIN(at) FROM mcp_clock c WHERE c.machine_id=s.machine_id AND c.at>=MAX(s.first_at,?) AND c.at<=MIN(s.last_at,?)) ELSE s.last_at END,
 CASE WHEN s.resolution=60 THEN (SELECT MAX(at) FROM mcp_clock c WHERE c.machine_id=s.machine_id AND c.at>=MAX(s.first_at,?) AND c.at<=MIN(s.last_at,?)) ELSE s.last_at END
 FROM mcp_states s JOIN mcp_groups g ON g.id=s.group_id JOIN mcp_labels l ON l.id=s.labels_id
 WHERE s.machine_id=? AND s.first_at>=? AND s.first_at<=? AND s.last_at>=?
 ORDER BY s.first_at,s.group_id`, unix(since), unix(until), unix(since), unix(until), q.machineID, unix(since.Truncate(time.Hour)), unix(until), unix(since))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r mcpState
		var start int64
		var first, last *int64
		err := rows.Scan(&r.id, &r.first, &r.last, &r.sample.Host.PID, &start, &r.sample.Key, &r.sample.Host.Name, &r.sample.Host.Exe, &r.sample.Host.Cwd, &r.sample.Name, &r.sample.Count, &r.sample.Footprint, &r.members, &r.sample.ResolutionSeconds, &first, &last)
		if err != nil {
			return err
		}
		if first == nil || last == nil || *last > unix(until) {
			continue
		}
		r.sample.Host.StartedAt = fromUnix(start)
		if err := json.Unmarshal([]byte(r.members), &r.sample.Members); err != nil {
			return err
		}
		r.sample.At = fromUnix(*first)
		if err := visit(r.sample); err != nil {
			return err
		}
		if *last != *first {
			r.sample.At = fromUnix(*last)
			if err := visit(r.sample); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// pruneMCP keeps hourly last observations beyond the full-detail window.
// Runs never cross an hour, so pruning cannot erase the beginning of a live run.
func (tx *Tx) pruneMCP(now time.Time) error {
	day := unix(now.Add(-24 * time.Hour).Truncate(time.Hour))
	month := unix(now.Add(-30 * 24 * time.Hour).Truncate(time.Hour))
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM mcp_states WHERE machine_id=? AND first_at<?`, []any{tx.machineID, month}},
		{`DELETE FROM mcp_states WHERE machine_id=? AND first_at<? AND (group_id,first_at) NOT IN (SELECT group_id,MAX(first_at) FROM mcp_states WHERE machine_id=? AND first_at>=? AND first_at<? GROUP BY group_id,first_at/?)`, []any{tx.machineID, day, tx.machineID, month, day, int64(time.Hour)}},
		{`UPDATE mcp_states SET resolution=3600 WHERE machine_id=? AND first_at<? AND resolution<>3600`, []any{tx.machineID, day}},
		{`DELETE FROM mcp_clock WHERE machine_id=? AND at<?`, []any{tx.machineID, day}},
		{`DELETE FROM mcp_groups WHERE machine_id=? AND NOT EXISTS(SELECT 1 FROM mcp_states s WHERE s.group_id=mcp_groups.id)`, []any{tx.machineID}},
		{`DELETE FROM mcp_labels WHERE machine_id=? AND NOT EXISTS(SELECT 1 FROM mcp_states s WHERE s.labels_id=mcp_labels.id)`, []any{tx.machineID}},
	} {
		if err := tx.exec(stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}
