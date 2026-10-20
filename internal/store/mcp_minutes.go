package store

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/labstack/driga/internal/model"
)

const mcpMinuteColumns = `sampled_at,host_pid,host_start,command_key,host_name,host_exe,host_cwd,name,count,footprint,members`

func (q *querier) MCPMinuteDue(at time.Time) (bool, error) {
	raw, err := q.Meta("machine:mcp_minute")
	if err != nil && err != ErrNotFound {
		return false, err
	}
	previous, parseErr := strconv.ParseInt(raw, 10, 64)
	return parseErr != nil || unix(at.Truncate(time.Minute)) > previous, nil
}

func scanMCPMinute(s scanner) (model.MCPMinute, error) {
	var r model.MCPMinute
	var at, start int64
	var members string
	err := s.Scan(&at, &r.Host.PID, &start, &r.Key, &r.Host.Name, &r.Host.Exe, &r.Host.Cwd, &r.Name, &r.Count, &r.Footprint, &members)
	r.At, r.Host.StartedAt = fromUnix(at), fromUnix(start)
	if err == nil {
		err = json.Unmarshal([]byte(members), &r.Members)
	}
	return r, err
}

// ObserveMCP uses one process sample per minute, even with a viewer polling at
// five seconds. The durable gate survives collector restarts and empty samples.
// A disappearance gets one zero row, so a command that ends completely cannot
// retain an old leaking status. Only the previous minute's rows are read here.
func (tx *Tx) ObserveMCP(at time.Time, samples []model.MCPMinute) error {
	minute := unix(at.Truncate(time.Minute))
	due, err := tx.MCPMinuteDue(at)
	if err != nil || !due {
		return err
	}
	previous, err := readMany(&tx.querier, `SELECT `+mcpMinuteColumns+` FROM mcp_minutes WHERE machine_id=? AND minute=(SELECT MAX(minute) FROM mcp_minutes WHERE machine_id=?)`, []any{tx.machineID, tx.machineID}, scanMCPMinute)
	if err != nil {
		return err
	}
	type key struct {
		pid     int32
		start   int64
		command string
	}
	seen := make(map[key]bool, len(samples))
	write := func(s model.MCPMinute) error {
		members, err := json.Marshal(s.Members)
		if err != nil {
			return err
		}
		return tx.exec(`INSERT OR IGNORE INTO mcp_minutes(machine_id,minute,host_pid,host_start,command_key,sampled_at,host_name,host_exe,host_cwd,name,count,footprint,members) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, tx.machineID, minute, s.Host.PID, unix(s.Host.StartedAt), s.Key, unix(at), s.Host.Name, s.Host.Exe, s.Host.Cwd, s.Name, s.Count, s.Footprint, string(members))
	}
	for _, s := range samples {
		seen[key{s.Host.PID, unix(s.Host.StartedAt), s.Key}] = true
		if err := write(s); err != nil {
			return err
		}
	}
	for _, s := range previous {
		if s.Count == 0 || seen[key{s.Host.PID, unix(s.Host.StartedAt), s.Key}] {
			continue
		}
		s.Count, s.Footprint, s.Members = 0, 0, nil
		if err := write(s); err != nil {
			return err
		}
	}
	return tx.SetMeta("machine:mcp_minute", strconv.FormatInt(minute, 10))
}

// ScanMCPMinutes streams retained evidence in time order: memory is bounded by
// the active group memberships, not thirty days of process samples.
func (q *querier) ScanMCPMinutes(until time.Time, visit func(model.MCPMinute) error) error {
	rows, err := q.conn.QueryContext(q.ctx, `SELECT `+mcpMinuteColumns+` FROM mcp_minutes WHERE machine_id=? AND sampled_at<=? ORDER BY minute,host_pid,host_start,command_key`, q.machineID, unix(until))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanMCPMinute(rows)
		if err != nil {
			return err
		}
		if err := visit(r); err != nil {
			return err
		}
	}
	return rows.Err()
}
