package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/store"
)

func TestReclaimHistoryJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DRIGA_DATA_DIR", dir)
	if err := reclaim.Append(dir, reclaim.Entry{Kind: "process", ID: "fixture-tree", Status: "partial", Survivors: []int32{42}}); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"reclaim", "history", "--json", "--kind", "process"}, "test", nil, &out, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s out=%s", code, &stderr, &out)
	}
	var envelope struct {
		Schema int             `json:"schema"`
		Data   reclaim.History `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Schema != 2 || len(envelope.Data.Entries) != 1 || envelope.Data.Entries[0].Status != "partial" {
		t.Fatalf("%+v", envelope)
	}
}

func TestReclaimHistoryInvalidLimit(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := run([]string{"reclaim", "history", "--limit", "0"}, "test", nil, &out, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
}

func TestReclaimPlanReadsOneCommittedFrame(t *testing.T) {
	readEnv(t)
	base := time.Now().Add(-time.Hour)
	write := func(tx *store.Tx, n int) error {
		start := base.Add(time.Duration(n%2) * time.Second)
		if err := tx.UpsertMachine(model.Machine{OS: runtime.GOOS}); err != nil {
			return err
		}
		if err := tx.ReplaceProcs([]model.Proc{{PID: 4242, PPID: 1, StartTime: start, RSS: 100}}); err != nil {
			return err
		}
		if err := tx.ReplaceOrphans([]model.Orphan{{ID: "fixture", RootPID: 4242, StartTime: start, PIDs: []int32{4242}, ProcCount: 1, Verified: true}}); err != nil {
			return err
		}
		return tx.SetSourceOK("procs", time.Now())
	}
	st := readDB(t, func(tx *store.Tx) error { return write(tx, 0) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		for n := 1; ctx.Err() == nil; n++ {
			if err := st.Tx(context.Background(), func(tx *store.Tx) error { return write(tx, n) }); err != nil {
				done <- err
				return
			}
			runtime.Gosched()
		}
		done <- nil
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for range 100 {
		e, err := readReclaimPlan(context.Background(), st, "daemon", nil)
		if err != nil {
			t.Fatal(err)
		}
		rows := e.Data.([]reclaim.Opportunity)
		if len(rows) != 1 || !rows[0].ResourcesKnown || rows[0].Memory.RSS != 100 {
			t.Fatalf("mixed collector frames: %+v", rows)
		}
		if e.ObservedAt.Before(e.Freshness["procs"].LastOKAt) {
			t.Fatal("envelope timestamp precedes source sample")
		}
	}
}
