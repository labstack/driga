package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestObservationMinutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.db")
	s := openTest(t, path, ReadWrite)
	r := openTest(t, path, ReadOnly)
	now := time.Date(2026, 10, 19, 12, 0, 5, 123000000, time.UTC)
	write := func(at time.Time, log, cpu int64) {
		t.Helper()
		mustTx(t, s, func(tx *Tx) error { return tx.Observe(42, at, log, cpu) })
	}
	write(now, 12, 30)
	write(now.Add(time.Second), 8, 0)
	write(now.Add(2*time.Second), 0, 10)
	rows, err := s.ObservationMinutes(42, now)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	row := rows[0]
	if row.LogBytes != 20 || row.CPUMS != 40 || row.Samples != 3 || !row.LastGrowthAt.Equal(now.Add(time.Second)) || row.LastGrowthBytes != 8 {
		t.Fatal(row)
	}
	before, _ := r.DataVersion(context.Background())
	write(now, 0, 0)
	after, _ := r.DataVersion(context.Background())
	if before != after {
		t.Fatal("idle observation changed DataVersion")
	}
	other, err := s.ReadMachine("other").ObservationMinutes(42, now)
	if err != nil || len(other) != 0 {
		t.Fatalf("machine leaked: %+v %v", other, err)
	}
	write(now.Add(-31*24*time.Hour), 1, 0)
	write(now.Add(-30*24*time.Hour).Truncate(time.Minute), 2, 0)
	mustTx(t, s, func(tx *Tx) error { return tx.Prune(now) })
	rows, err = s.ObservationMinutes(42, now)
	if err != nil || len(rows) != 2 {
		t.Fatalf("retention %+v %v", rows, err)
	}
	mustTx(t, s, func(tx *Tx) error { return tx.PruneSnapshot(now) })
	rows, err = s.ObservationMinutes(42, now)
	if err != nil || len(rows) != 1 {
		t.Fatalf("snapshot retention %+v %v", rows, err)
	}
}

func TestObservationSurvivesIdentityReset(t *testing.T) {
	s := openTest(t, "", Memory)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mustTx(t, s, func(tx *Tx) error { return tx.Observe(42, at, 100, 0) })
	if err := s.ResetMachineID(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ObservationMinutes(42, at)
	if err != nil || len(rows) != 1 {
		t.Fatalf("identity reset lost observations: %+v %v", rows, err)
	}
}
