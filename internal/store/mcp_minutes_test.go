package store

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
)

func mcpRows(t *testing.T, s interface {
	ScanMCPMinutes(time.Time, func(model.MCPMinute) error) error
}, at time.Time) []model.MCPMinute {
	t.Helper()
	var rows []model.MCPMinute
	if err := s.ScanMCPMinutes(at, func(s model.MCPMinute) error { rows = append(rows, s); return nil }); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestMCPPruningRebasesObservedWindow(t *testing.T) {
	s := openTest(t, "", Memory)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ps := []model.Proc{{PID: 100, Cmdline: "claude", StartTime: at.Add(-time.Hour * 24)}}
	for i, minutes := range []int{-61, -60, -59, -30, 0} {
		now := at.Add(time.Duration(minutes) * time.Minute)
		if i == 1 {
			ps = ps[:1]
		} // an end outside the snapshot window
		ps = append(ps, model.Proc{PID: int32(i + 1), PPID: 100, Cmdline: "node /opt/server.js", StartTime: now})
		v := derive.MCPServers(ps, nil, now)
		mustTx(t, s, func(tx *Tx) error { return tx.ObserveMCP(now, v.Samples) })
	}
	trends := func() []derive.MCPTrend {
		b := derive.NewMCPTrendBuilder()
		for _, row := range mcpRows(t, s, at) {
			b.Add(row)
		}
		return b.Trends()
	}
	if got := trends()[0]; got.State != "churning" || got.EndedCount != 1 {
		t.Fatal(got)
	}
	mustTx(t, s, func(tx *Tx) error { return tx.PruneSnapshot(at) })
	got := trends()[0]
	if got.State != "leaking" || got.StartedCount != 2 || got.EndedCount != 0 || !got.FirstSample.Equal(at.Add(-59*time.Minute)) || got.RatePerHour != 2/(59.0/60) {
		t.Fatal(got)
	}
}

func TestMCPMinutesRetentionAndCadence(t *testing.T) {
	s := openTest(t, "", Memory)
	at := time.Date(2026, 10, 20, 12, 0, 5, 0, time.UTC)
	sample := model.MCPMinute{Key: "command:server", Name: "server", Host: model.MCPHost{PID: 100, StartedAt: at.Add(-32 * 24 * time.Hour)}, Count: 1, Footprint: 42, Members: []model.ProcessIdentity{{PID: 1, StartedAt: at.Add(-time.Hour)}}}
	write := func(at time.Time, samples []model.MCPMinute) {
		t.Helper()
		mustTx(t, s, func(tx *Tx) error { return tx.ObserveMCP(at, samples) })
	}
	for _, ts := range []time.Time{at.Add(-31 * 24 * time.Hour), at.Add(-30 * 24 * time.Hour).Truncate(time.Minute), at.Add(-60 * time.Minute), at.Add(-59 * time.Minute).Truncate(time.Minute), at} {
		write(ts, []model.MCPMinute{sample})
	}
	sample.Count = 2
	write(at.Add(time.Second), []model.MCPMinute{sample})
	rows := mcpRows(t, s, at.Add(time.Second))
	if len(rows) != 5 || rows[4].Count != 1 || rows[4].Footprint != 42 {
		t.Fatal(rows)
	}
	if got := mcpRows(t, s.ReadMachine("peer"), at); len(got) != 0 {
		t.Fatal(got)
	}
	mustTx(t, s, func(tx *Tx) error { return tx.Prune(at) })
	if got := mcpRows(t, s, at); len(got) != 4 {
		t.Fatal(got)
	}
	mustTx(t, s, func(tx *Tx) error { return tx.PruneSnapshot(at) })
	if got := mcpRows(t, s, at); len(got) != 2 {
		t.Fatal(got)
	}
	write(at.Add(time.Minute), nil)
	write(at.Add(2*time.Minute), nil)
	rows = mcpRows(t, s, at.Add(2*time.Minute))
	if len(rows) != 3 || rows[2].Count != 0 || len(rows[2].Members) != 0 {
		t.Fatal(rows)
	}
	if err := s.ResetMachineID(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := mcpRows(t, s, at.Add(2*time.Minute)); len(got) != 3 {
		t.Fatal(got)
	}
}
