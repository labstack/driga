package view

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestFlowFamily(t *testing.T) {
	var lead int64
	st := fixture(t, func(tx *store.Tx) error {
		var err error
		lead, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "lead", PID: 10, RegistryStatus: "busy", StartedAt: testNow.Add(-time.Hour), LastActivityAt: testNow}, Label: "working"})
		if err != nil {
			return err
		}
		for i, name := range []string{"worker", "finished", "hosted"} {
			s := model.Session{ParentID: &lead, SessionObs: model.SessionObs{Adapter: "codex", ExternalID: name, PID: int32(11 + i), Hosted: i == 2, StartedAt: testNow.Add(-time.Hour), LastActivityAt: testNow.Add(-2 * time.Minute)}}
			s.Label = []string{"working", "finished", "idle"}[i]
			if i == 1 {
				end := testNow.Add(-time.Minute)
				s.EndedAt = &end
				s.PID = 0
			}
			if _, err = tx.UpsertSession(s); err != nil {
				return err
			}
			for _, age := range []time.Duration{9 * time.Minute, 2 * time.Minute, 2 * time.Minute} {
				if err = tx.InsertEvent(model.Event{Adapter: "codex", ExternalID: name, Kind: "tool", TS: testNow.Add(-age)}); err != nil {
					return err
				}
			}
		}
		return tx.ReplaceProcs([]model.Proc{{PID: 10, RSS: 100}, {PID: 11, PPID: 10}, {PID: 99, PPID: 10}})
	})
	v, err := Flow(st, Filter{}, 0, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if v.Lead == nil || v.Lead.ID != lead || len(v.Children) != 3 {
		t.Fatalf("%+v", v)
	}
	if v.Mixer.Verdict != v.Lead.Label {
		t.Fatal(v)
	}
	for i, c := range v.Children {
		if c.Label != []string{"working", "finished", "idle"}[i] {
			t.Fatal(c.Label)
		}
		if i == 2 && c.Kind != "hosted" {
			t.Fatal("hosted child lost")
		}
		if i == 1 && c.Elapsed != 59*time.Minute {
			t.Fatal(c.Elapsed)
		}
		if len(c.Activity) != 10 || c.Activity[0] != 1 || c.Activity[7] != 2 {
			t.Fatal(c.Activity)
		}
	}
	raw, _ := json.Marshal(v)
	var obj map[string]any
	json.Unmarshal(raw, &obj)
	l := obj["lead"].(map[string]any)
	for _, key := range []string{"model", "input_tokens", "output_tokens", "cost_usd"} {
		if _, ok := l[key]; ok {
			t.Fatalf("fabricated %s: %s", key, raw)
		}
	}
	if len(v.Tail) != 9 {
		t.Fatal(v.Tail)
	}
	selected, err := Flow(st, Filter{}, v.Children[0].ID, testNow)
	if err != nil || selected.Lead.ID != lead {
		t.Fatal(selected, err)
	}
}

func TestFlowRetainedUsageAndFamilyTail(t *testing.T) {
	var id int64
	st := fixture(t, func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "lead", PID: 1, RegistryStatus: "busy", LastActivityAt: testNow}, Label: "working"})
		if err != nil {
			return err
		}
		in, out := int64(120), int64(0)
		cost := 0.12
		for _, e := range []model.Event{{Adapter: "claude", ExternalID: "lead", TS: testNow.Add(-2 * time.Minute), Kind: "otlp:api_request", Metadata: &model.EventMetadata{Model: "model-test", InputTokens: &in, OutputTokens: &out, CostUSD: &cost}}, {Adapter: "claude", ExternalID: "lead", TS: testNow.Add(-time.Minute), Kind: "otlp:tool_result", Metadata: &model.EventMetadata{Tool: "Read"}}} {
			if err = tx.InsertEvent(e); err != nil {
				return err
			}
		}
		for i := 0; i < 220; i++ {
			if err = tx.InsertEvent(model.Event{Adapter: "codex", ExternalID: "unrelated", TS: testNow, Kind: "tool"}); err != nil {
				return err
			}
		}
		return nil
	})
	v, err := Flow(st, Filter{}, id, testNow)
	if err != nil {
		t.Fatal(err)
	}
	n := v.Lead
	if n.Model != "model-test" || n.LastTool != "Read" || n.InputTokens == nil || *n.InputTokens != 120 || n.OutputTokens == nil || *n.OutputTokens != 0 || n.CostUSD == nil || *n.CostUSD != .12 || n.Mem != nil || n.Helpers != nil {
		t.Fatal(n)
	}
	if len(v.Tail) != 2 || v.EventsPerMinute != .4 {
		t.Fatal(v.Tail, v.EventsPerMinute)
	}
	if _, err := Flow(st, Filter{Project: "/different"}, id, testNow); err == nil {
		t.Fatal("project scope ignored")
	}
}
