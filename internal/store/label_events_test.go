package store

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestLabelEventsProjectionAndOrder(t *testing.T) {
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if empty, err := st.LabelEvents(); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	err = st.Tx(context.Background(), func(tx *Tx) error {
		for _, e := range []model.Event{
			{Adapter: "claude", ExternalID: "one", Kind: "Stop", TS: at.Add(-time.Second)},
			{Adapter: "claude", ExternalID: "one", Kind: "otlp:api_error", TS: at, Summary: "older insertion"},
			{Adapter: "claude", ExternalID: "one", Kind: "Stop", TS: at},
			{Adapter: "claude", ExternalID: "one", Kind: "otlp:api_error", TS: at, Summary: "newer insertion"},
			{Adapter: "claude", ExternalID: "one", Kind: "arbitrary:hook", TS: at},
			{Adapter: "claude", ExternalID: "two", Kind: "Stop", TS: at},
			{Adapter: "codex", ExternalID: "one", Kind: "Stop", TS: at},
			{Adapter: "claude", ExternalID: "one", Kind: "at-zero", TS: time.Time{}},
			{Adapter: "claude", ExternalID: "one", Kind: "before-zero", TS: time.Time{}.Add(-time.Second)},
		} {
			if err := tx.InsertEvent(e); err != nil {
				return err
			}
		}
		if err := tx.ForMachine("remote").InsertEvent(model.Event{Adapter: "claude", ExternalID: "one", Kind: "remote", TS: at.Add(time.Hour)}); err != nil {
			return err
		}
		// Labeling does not decode protocol metadata, even when older stored
		// metadata is malformed. Non-error summary content is not an input.
		return tx.exec("UPDATE events SET metadata='invalid JSON', summary=CASE WHEN kind='otlp:api_error' THEN summary ELSE 'discarded content' END")
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.LabelEvents()
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Event{
		{MachineID: st.MachineID(), Adapter: "codex", ExternalID: "one", Kind: "Stop", TS: at},
		{MachineID: st.MachineID(), Adapter: "claude", ExternalID: "two", Kind: "Stop", TS: at},
		{MachineID: st.MachineID(), Adapter: "claude", ExternalID: "one", Kind: "arbitrary:hook", TS: at},
		{MachineID: st.MachineID(), Adapter: "claude", ExternalID: "one", Kind: "otlp:api_error", TS: at, Summary: "newer insertion"},
		{MachineID: st.MachineID(), Adapter: "claude", ExternalID: "one", Kind: "Stop", TS: at},
		{MachineID: st.MachineID(), Adapter: "claude", ExternalID: "one", Kind: "at-zero", TS: time.Time{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("label event projection/order:\n got %+v\nwant %+v", got, want)
	}
}

func labelEventsCostFixture(t testing.TB) *Store {
	t.Helper()
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	kinds := []string{"Stop", "PostToolUse", "otlp:api_request", "otlp:api_error"}
	err = st.Tx(context.Background(), func(tx *Tx) error {
		for session := range 64 {
			for i := range 200 {
				// Match the store's per-session retained window without measuring
				// panel projection and pruning work in this read benchmark's setup.
				if err := tx.exec("INSERT INTO events(machine_id,adapter,external_id,ts,kind,summary,metadata) VALUES(?,?,?,?,?,?,?)",
					tx.machineID, "claude", fmt.Sprint(session), unix(at.Add(time.Duration(i)*time.Second)), kinds[i%len(kinds)], strings.Repeat("event summary ", 12), `{"model":"fixture","input_tokens":120,"output_tokens":40}`); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestLabelEventsReadCost(t *testing.T) {
	st := labelEventsCostFixture(t)
	read := func(projected bool) func() {
		return func() {
			var err error
			if projected {
				_, err = st.LabelEvents()
			} else {
				_, err = st.LatestEvents(time.Time{})
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	full := testing.AllocsPerRun(2, read(false))
	projected := testing.AllocsPerRun(2, read(true))
	t.Logf("full-history allocations=%.0f, label projection=%.0f", full, projected)
	if projected > full/4 {
		t.Fatalf("label projection retained full-history allocation cost: %.0f vs %.0f", projected, full)
	}
}

func BenchmarkLabelEvents(b *testing.B) {
	st := labelEventsCostFixture(b)
	for _, projected := range []bool{false, true} {
		b.Run(map[bool]string{false: "full-history", true: "label-projection"}[projected], func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				var err error
				if projected {
					_, err = st.LabelEvents()
				} else {
					_, err = st.LatestEvents(time.Time{})
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
