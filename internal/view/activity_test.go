package view

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestActivityBucketsGapsEventsAndScope(t *testing.T) {
	now := testNow.Add(35 * time.Second)
	start := testNow.Add(-59 * time.Minute)
	s := fixture(t, func(tx *store.Tx) error {
		for _, id := range []string{tx.MachineID(), "remote"} {
			w := tx.ForMachine(id)
			factor := 1.0
			if id == "remote" {
				factor = 10
			}
			if err := w.UpsertMachine(model.Machine{OS: "linux", Name: id}); err != nil {
				return err
			}
			for i, at := range []time.Time{start.Add(-time.Second), start, start.Add(20 * time.Second), testNow, now.Add(time.Second)} {
				if err := w.InsertSample(model.MachineSample{TS: at, Load1: factor * float64(i+1), MemUsed: 10, MemTotal: 20, SwapUsed: 3, SwapTotal: 4, PressureFree: 50}); err != nil {
					return err
				}
			}
			if id == "remote" {
				if err := w.InsertEvent(model.Event{Adapter: "test", ExternalID: "extra", TS: start.Add(30 * time.Second), Kind: "test"}); err != nil {
					return err
				}
			}
			for i, at := range []time.Time{start.Add(-time.Nanosecond), start, start.Add(59 * time.Second), start.Add(time.Minute), now, now.Add(time.Nanosecond)} {
				if err := w.InsertEvent(model.Event{Adapter: "test", ExternalID: string(rune('a' + i)), TS: at, Kind: "test"}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	v, err := Activity(s, Filter{}, now)
	if err != nil || len(v.Machines) != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	for _, m := range v.Machines {
		factor, count := 1.0, 2
		if m.MachineID == "remote" {
			factor, count = 10, 3
		}
		if len(m.Buckets) != 60 {
			t.Fatalf("buckets: %d", len(m.Buckets))
		}
		b := m.Buckets
		if !b[0].TS.Equal(start) || !b[59].TS.Equal(testNow) || b[0].Sample == nil || b[59].Sample == nil || b[0].Sample.Load1 != factor*3 || b[59].Sample.Load1 != factor*4 {
			t.Fatalf("buckets: %+v", b)
		}
		if b[1].Sample != nil || b[1].Events != 1 || b[0].Events != count || b[59].Events != 1 || b[2].Events != 0 {
			t.Fatalf("gaps/counts: %+v", b)
		}
		if b[0].Sample.MemTotal != 20 || b[0].Sample.PressureFree != 50 {
			t.Fatal(b[0])
		}
		encoded, _ := json.Marshal(b[1])
		if !strings.Contains(string(encoded), `"sample":null`) {
			t.Fatal(string(encoded))
		}
	}
	for _, scope := range []store.Scope{{Machines: []string{"remote"}}, {Machines: []string{s.MachineID()}}, {Machines: []string{}}} {
		v, err := Activity(s, Filter{Scope: scope}, now)
		if err != nil || len(v.Machines) != len(scope.Machines) {
			t.Fatalf("scope: %+v %v", v, err)
		}
		if len(v.Machines) > 0 && v.Machines[0].MachineID != scope.Machines[0] {
			t.Fatal(v)
		}
	}
}

func TestActivityWithoutSamples(t *testing.T) {
	s := fixture(t, func(*store.Tx) error { return nil })
	v, err := Activity(s, Filter{}, testNow)
	if err != nil || len(v.Machines) != 1 || len(v.Machines[0].Buckets) != 60 {
		t.Fatalf("%+v %v", v, err)
	}
	for _, b := range v.Machines[0].Buckets {
		if b.Sample != nil {
			t.Fatal("invented sample", b)
		}
	}
}
