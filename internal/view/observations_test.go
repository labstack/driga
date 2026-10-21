package view

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestFlowObservedBuckets(t *testing.T) {
	var id int64
	st := fixture(t, func(tx *store.Tx) (err error) {
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "observed", PID: 1}})
		if err != nil {
			return err
		}
		if _, err = tx.UpsertSession(model.Session{ParentID: &id, SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "never"}}); err != nil {
			return err
		}
		for _, v := range []struct {
			age        time.Duration
			bytes, cpu int64
		}{{60 * time.Minute, 999, 3}, {59 * time.Minute, 12, 5}, {0, 40, 20}} {
			if err = tx.Observe(id, testNow.Add(-v.age), v.bytes, v.cpu); err != nil {
				return err
			}
		}
		return nil
	})
	v, err := Flow(st, Filter{}, id, testNow)
	if err != nil {
		t.Fatal(err)
	}
	o := v.Lead.Observed
	if o == nil || len(o.LogBytesPerMin) != 60 || len(o.CPUMSPerMin) != 60 {
		t.Fatalf("observed=%+v", o)
	}
	if o.LogBytesPerMin[0] != 12 || o.LogBytesPerMin[1] != 0 || o.LogBytesPerMin[59] != 40 || o.CPUMSPerMin[0] != 5 || o.CPUMSPerMin[59] != 20 || !o.LastGrowthAt.Equal(testNow) || o.LastGrowthBytes != 40 {
		t.Fatal(o)
	}
	if v.Children[0].Observed != nil {
		t.Fatal("fabricated observation")
	}
	raw, _ := json.Marshal(v)
	var obj map[string]any
	json.Unmarshal(raw, &obj)
	if child := obj["children"].([]any)[0].(map[string]any); child["observed"] != nil {
		t.Fatal(child)
	}
	later, err := Flow(st, Filter{}, id, testNow.Add(2*time.Hour))
	if err != nil || later.Lead.Observed == nil || later.Lead.Observed.LogBytesPerMin[59] != 0 || later.Lead.Observed.LastGrowthBytes != 40 {
		t.Fatal(later, err)
	}
}

func TestObservedSchemaContract(t *testing.T) {
	raw, err := os.ReadFile("../../docs/contracts/schema2.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err = json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Defs["flow_observed"] == nil {
		t.Fatal("missing flow_observed contract")
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(map[string]any{"schema": float64(2), "data": map[string]any{"lead": map[string]any{}, "children": []any{}}}); err == nil {
		t.Fatal("envelope accepts flow node without observed")
	}
	// Resolve the documented flow data definition as a separate test root.
	s.Ref = "#/$defs/flow"
	s.Required = nil
	s.AnyOf = nil
	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	o := map[string]any{"log_bytes_per_min": make([]int64, 60), "cpu_ms_per_min": make([]int64, 60), "last_growth_at": "2026-10-19T19:00:00Z", "last_growth_bytes": 12}
	node := map[string]any{"observed": o}
	data := map[string]any{"lead": node, "children": []any{map[string]any{"observed": nil}}}
	check := func(valid bool) {
		t.Helper()
		b, _ := json.Marshal(data)
		var v any
		json.Unmarshal(b, &v)
		if err := r.Validate(v); (err == nil) != valid {
			t.Fatalf("valid=%v err=%v", valid, err)
		}
	}
	check(true)
	o["log_bytes_per_min"] = []int64{1}
	check(false)
	o["log_bytes_per_min"] = make([]int64, 60)
	o["last_growth_bytes"] = -1
	check(false)
	o["last_growth_bytes"] = 12
	o["last_growth_at"] = "2026-10-19T12:00:00-07:00"
	check(false)
	o["last_growth_at"] = "2026-10-19T19:00:00Z"
	delete(node, "observed")
	check(false)
}
