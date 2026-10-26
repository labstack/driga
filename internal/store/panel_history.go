package store

import (
	"strings"
	"time"
)

// PanelPoll retains only state evidence that cannot be reconstructed from inbox
// intervals. It is receiver-local, including the receiver's peer reachability.
type PanelPoll struct {
	TS    time.Time
	Worst string
	Stuck int
}

// RecordPanelPolls records one committed collector poll for each known machine.
// Keep an hour plus each machine's last 30 polls, including across long gaps.
func (tx *Tx) RecordPanelPolls(now time.Time) error {
	machines, err := tx.Machines()
	if err != nil {
		return err
	}
	for _, m := range machines {
		q := tx.ForMachine(m.MachineID)
		ss, err := q.Sessions(SessionFilter{})
		if err != nil {
			return err
		}
		p := PanelPoll{TS: now, Worst: "quiet"}
		rank := map[string]int{"quiet": 0, "idle": 1, "working": 2, "stuck": 4, "waiting": 3}
		for _, s := range ss {
			if (s.Hosted || strings.HasPrefix(s.Origin, "hosted")) && now.Sub(s.LastActivityAt) > time.Hour {
				continue
			}
			state := strings.TrimPrefix(s.Label, "~")
			if state == "stalled?" {
				p.Stuck++
				state = "stuck"
			}
			if state == "dead" {
				state = "stuck"
			}
			if rank[state] > rank[p.Worst] {
				p.Worst = state
			}
		}
		if m.MachineID != tx.MachineID() && (m.State != "live" || m.LastContactAt.IsZero() || now.Sub(m.LastContactAt) >= 45*time.Second) {
			p.Worst = "offline"
		}
		if err := q.exec("INSERT OR REPLACE INTO panel_polls(machine_id,ts,worst,stuck) VALUES(?,?,?,?)", q.machineID, unix(now), p.Worst, p.Stuck); err != nil {
			return err
		}
		if err := q.exec("DELETE FROM panel_polls WHERE machine_id=? AND ts<? AND ts NOT IN (SELECT ts FROM panel_polls WHERE machine_id=? ORDER BY ts DESC LIMIT 30) AND ts != COALESCE((SELECT MAX(ts) FROM panel_polls WHERE machine_id=? AND ts<?),0)", q.machineID, unix(now.Add(-time.Hour)), q.machineID, q.machineID, unix(now.Add(-time.Hour))); err != nil {
			return err
		}
	}
	return nil
}

// PanelPolls returns the retained polls no later than now, oldest first.
func (q *querier) PanelPolls(now time.Time) ([]PanelPoll, error) {
	return readMany(q, "SELECT ts,worst,stuck FROM panel_polls WHERE machine_id=? AND ts<=? ORDER BY ts", []any{q.machineID, unix(now)}, func(s scanner) (PanelPoll, error) {
		var p PanelPoll
		var at int64
		err := s.Scan(&at, &p.Worst, &p.Stuck)
		p.TS = fromUnix(at)
		return p, err
	})
}
