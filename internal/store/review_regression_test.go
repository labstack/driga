package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

type countedPanelDB struct {
	queryDB
	sessionReads int
}

func (c *countedPanelDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "FROM sessions") {
		c.sessionReads++
	}
	return c.queryDB.QueryContext(ctx, query, args...)
}

func TestPanelPollsReadSessionsOnce(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Date(2026, 3, 14, 15, 30, 0, 0, time.UTC)
	want := map[string]PanelPoll{
		st.MachineID(): {TS: now, Worst: "stuck", Stuck: 1},
		"peer-a":       {TS: now, Worst: "working"},
		"peer-b":       {TS: now, Worst: "waiting"},
	}
	mustTx(t, st, func(tx *Tx) error {
		for machine, state := range map[string]string{st.MachineID(): "stuck", "peer-a": "working", "peer-b": "waiting"} {
			q := tx.ForMachine(machine)
			if err := q.UpsertMachine(model.Machine{MachineID: machine, State: "live", LastContactAt: now}); err != nil {
				return err
			}
			if _, err := q.UpsertSession(model.Session{Label: state, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "shared"}}); err != nil {
				return err
			}
		}
		// Stale hosted and ended sessions must not inflate indexed counts.
		if _, err := tx.UpsertSession(model.Session{Label: "stuck", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "stale", Hosted: true, LastActivityAt: now.Add(-2 * time.Hour)}}); err != nil {
			return err
		}
		if _, err := tx.UpsertSession(model.Session{Label: "stuck", EndedAt: &now, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "ended"}}); err != nil {
			return err
		}
		count := &countedPanelDB{queryDB: tx.conn}
		tx.conn = count
		if err := tx.RecordPanelPolls(now); err != nil {
			return err
		}
		if count.sessionReads != 1 {
			return fmt.Errorf("session reads=%d, want one machine batch", count.sessionReads)
		}
		return nil
	})
	for machine, expected := range want {
		polls, err := st.ReadMachine(machine).PanelPolls(now)
		if err != nil || len(polls) != 1 || polls[0] != expected {
			t.Fatalf("machine %s: polls=%+v want=%+v err=%v", machine, polls, expected, err)
		}
	}
}

func TestPruneUnclosedPromptIntervals(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Date(2026, 3, 14, 15, 30, 0, 0, time.UTC)
	cutoff, oldClosed, recentClosed := now.Add(-time.Hour), now.Add(-2*time.Hour), now.Add(-5*time.Minute)
	mustTx(t, st, func(tx *Tx) error {
		for i, entry := range []struct {
			start time.Time
			end   *time.Time
		}{
			{now.Add(-2 * time.Hour), nil},
			{cutoff, nil},
			{now, nil},
			{now.Add(-3 * time.Hour), &oldClosed},
			{now.Add(-3 * time.Hour), &recentClosed},
			{now.Add(-3 * time.Hour), &cutoff},
		} {
			var ended any
			if entry.end != nil {
				ended = unix(*entry.end)
			}
			if err := tx.exec("INSERT INTO prompt_intervals(machine_id,id,session_id,started_at,ended_at) VALUES(?,?,?,?,?)", tx.machineID, fmt.Sprint(i), i+1, unix(entry.start), ended); err != nil {
				return err
			}
		}
		return tx.Prune(now)
	})
	rows, err := st.PromptIntervals(time.Time{})
	if err != nil || len(rows) != 4 {
		t.Fatalf("retained prompt intervals=%+v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.SessionID == 1 || row.SessionID == 4 {
			t.Fatalf("expired prompt interval retained: %+v", row)
		}
	}
}
