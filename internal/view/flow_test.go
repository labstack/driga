package view

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestFlowUsesLabelPassInputs(t *testing.T) {
	var id int64
	st := fixture(t, func(tx *store.Tx) (err error) { id, err = flowtest.Seed(tx); return err })
	for _, now := range []time.Time{flowtest.Now, flowtest.Now.Add(5 * time.Second)} {
		v, err := Flow(st, Filter{}, id, now)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(flowtest.Expected())
		got, _ := json.Marshal(v.Mixer)
		if string(got) != string(want) || v.Lead.Label != "finished" {
			t.Errorf("flow replaced label-pass evidence at %s: got %s, want %s", now, got, want)
		}
	}
}

func TestFlowLegacyMixerPreservesStoredEvidence(t *testing.T) {
	mixed := flowtest.Expected()
	// A legacy record has no retained inputs, only the original explanation.
	raw, _ := json.Marshal(mixed)
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	delete(fields, "input")
	raw, _ = json.Marshal(fields)
	json.Unmarshal(raw, &mixed)
	var id int64
	st := fixture(t, func(tx *store.Tx) (err error) {
		id, err = tx.UpsertSession(model.Session{SessionObs: flowtest.Input().Obs, Label: mixed.Verdict, Mixer: &mixed})
		return err
	})
	v, err := Flow(st, Filter{}, id, flowtest.Now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Mixer, mixed) {
		t.Fatalf("legacy evidence changed: got %+v, want %+v", v.Mixer, mixed)
	}
}

func TestFlowQuietSplitUsesSnapshotTime(t *testing.T) {
	var id int64
	st := fixture(t, func(tx *store.Tx) (err error) { id, err = flowtest.Seed(tx); return err })
	for _, now := range []time.Time{flowtest.Now, flowtest.Now.Add(5 * time.Second)} {
		v, err := Flow(st, Filter{}, id, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, cap := range []int{1, 4, 5, 20} {
			w := v.Workers(cap, false, now)
			if len(w.Nodes) != min(cap, 5) || w.Quiet != 18-len(w.Nodes) {
				t.Errorf("cap %d at %s: visible=%d hidden=%d", cap, now, len(w.Nodes), w.Quiet)
			}
		}
		w := v.Workers(1, true, now)
		if len(w.Nodes) != 18 || w.Quiet != 0 {
			t.Fatal(w)
		}
	}
}

func TestFlowTitlesAndRelevance(t *testing.T) {
	var lead int64
	st := fixture(t, func(tx *store.Tx) error {
		var err error
		lead, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "lead", PID: 1}})
		if err != nil {
			return err
		}
		for i, obs := range []model.SessionObs{
			{Title: "  named  ", LastPrompt: "prompt", Cwd: "/repo", Branch: "main"},
			{Title: " ", LastPrompt: "retained prompt", Cwd: "/repo"},
			{Cwd: "/repo", Branch: "main"},
			{}, // No retained prompt means capture policy supplied no text.
		} {
			obs.Adapter, obs.ExternalID = "codex", fmt.Sprint(i)
			if _, err := tx.UpsertSession(model.Session{ParentID: &lead, SessionObs: obs, Label: "idle"}); err != nil {
				return err
			}
		}
		return nil
	})
	v, err := Flow(st, Filter{}, lead, testNow)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"named", "retained prompt", "repo · main", "codex thread"} {
		if v.Children[i].Title != want {
			t.Errorf("title %d = %q, want %q", i, v.Children[i].Title, want)
		}
	}
}

func TestFlowWorkerOrderAndCap(t *testing.T) {
	v := FlowView{}
	for i := 0; i < 11; i++ {
		v.Children = append(v.Children, FlowNode{ID: int64(i + 1), Kind: "hosted", Label: "idle", LastActivityAt: testNow.Add(-2 * time.Hour)})
	}
	v.Children = append(v.Children, FlowNode{ID: 20, Label: "idle", LastActivityAt: testNow}, FlowNode{ID: 21, Label: "working", LastActivityAt: testNow.Add(-time.Minute)}, FlowNode{ID: 22, Label: "waiting", LastActivityAt: testNow.Add(-2 * time.Minute)}, FlowNode{ID: 23, Label: "stalled", LastActivityAt: testNow.Add(-3 * time.Minute)}, FlowNode{ID: 24, Label: "finished", LastActivityAt: testNow.Add(-time.Second)}, FlowNode{ID: 25, Label: "idle", LastActivityAt: testNow.Add(-time.Minute)})
	w := v.Workers(5, false, testNow)
	if len(w.Nodes) != 5 || w.Quiet != 12 {
		t.Fatalf("%+v", w)
	}
	for i, id := range []int64{21, 22, 23, 20, 24} {
		if w.Nodes[i].ID != id {
			t.Fatal(w.Nodes)
		}
	}
	if all := v.Workers(5, true, testNow); len(all.Nodes) != 17 || all.Quiet != 0 {
		t.Fatal(all)
	}
	w = v.Workers(20, false, testNow)
	if len(w.Nodes) != 6 || w.Quiet != 11 {
		t.Fatal("old hosted threads escaped summary", w)
	}
}

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
