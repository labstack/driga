package view

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func shareFixture(t *testing.T, agent, editor bool) MachineView {
	t.Helper()
	at := testNow.Add(-time.Hour)
	ps := []model.Proc{{PID: 1, Exe: "/bin/editor", RSS: 100}, {PID: 10, PPID: 1, Exe: "/bin/worker", StartTime: at, RSS: 20, CPUPercent: 2}, {PID: 11, PPID: 10, Exe: "/bin/worker", StartTime: at.Add(time.Second), RSS: 30, CPUPercent: 3}, {PID: 12, PPID: 10, Exe: "/bin/worker", StartTime: at.Add(time.Second), RSS: 40, CPUPercent: 4}, {PID: 20, Cmdline: "codex app-server", Exe: "/bin/codex", RSS: 50}, {PID: 21, PPID: 1, Cmdline: "node /x/weather-mcp.js", RSS: 60}}
	if editor {
		ps[0].Exe = "/Applications/Editor.app/Contents/MacOS/Editor"
	}
	if !agent {
		ps = ps[:1]
	}
	st := fixture(t, func(tx *store.Tx) error {
		if agent {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "fixture", PID: 10, ProcStart: at, StartedAt: at, Cwd: "/projects/fixture"}}); err != nil {
				return err
			}
		}
		if err := tx.ReplaceProcs(ps); err != nil {
			return err
		}
		return tx.InsertSample(model.MachineSample{TS: testNow, MemUsed: 1000})
	})
	v, err := Machine(st, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestAgentShareTotalsEqualBuckets(t *testing.T) {
	v := shareFixture(t, true, true)
	if v.AgentShare.Memory+v.AgentShare.LikelyMemory+v.OtherApps.Memory != 300 || v.AgentShare.Memory != 140 || v.AgentShare.LikelyMemory != 60 || v.AgentShare.CPUPercent != 9 || v.AgentShare.Processes != 4 || v.AgentShare.MachineMemory != 1000 {
		t.Fatalf("%+v", v)
	}
	if len(v.AgentNoSession) != 1 || len(v.LikelyAgent) != 1 || len(v.OtherApps.Processes) != 0 {
		t.Fatalf("%+v", v)
	}
	if v.Machines[0].AgentShare != v.AgentShare {
		t.Fatal("fleet lost share")
	}
}
func TestSessionCostsIncludeHelpers(t *testing.T) {
	v := shareFixture(t, true, true)
	if len(v.SessionCosts) != 1 || len(v.SessionCosts[0].Processes) != 3 || v.SessionCosts[0].Memory != 90 || v.SessionCosts[0].Processes[0].PID != 12 {
		t.Fatal(v.SessionCosts)
	}
}
func TestSessionCostStartedFrom(t *testing.T) {
	for _, editor := range []bool{true, false} {
		v := shareFixture(t, true, editor)
		want := ""
		if editor {
			want = "Editor"
		}
		if v.SessionCosts[0].StartedFrom != want {
			t.Fatal(v.SessionCosts)
		}
	}
}
func TestAgentShareNoAgents(t *testing.T) {
	v := shareFixture(t, false, false)
	if v.AgentShare.Memory != 0 || len(v.SessionCosts) != 0 {
		t.Fatal(v)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(b, &obj)
	if string(obj["session_costs"]) != "[]" {
		t.Fatal(string(b))
	}
}
func TestMachineJSONAdditive(t *testing.T) {
	v := shareFixture(t, true, true)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(b, &obj)
	for _, key := range []string{"top_memory", "top_cpu", "agent_share", "session_costs", "agent_processes_without_session", "likely_agent_work", "other_apps"} {
		if _, ok := obj[key]; !ok {
			t.Error("missing", key)
		}
	}
	e, err := newEnvelopeLocal(fixture(t, nil), "snapshot", testNow, Intervals{}, v)
	if err != nil || e.Schema != 2 {
		t.Fatalf("schema=%d err=%v", e.Schema, err)
	}
}
