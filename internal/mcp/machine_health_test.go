package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestMachineLeakFieldsCLIParity(t *testing.T) {
	st, _ := fixture(t)
	t.Setenv("PATH", t.TempDir())
	at := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	ps := []model.Proc{{PID: 100, Cmdline: "codex app-server", Argv: []string{"codex", "app-server"}, StartTime: at.Add(-time.Hour)}}
	for i := range 3 {
		now := at.Add(time.Duration(i) * 80 * time.Minute)
		ps = append(ps, model.Proc{PID: int32(i + 1), PPID: 100, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, StartTime: now, RSS: 100})
		v := derive.MCPServers(ps, nil, now)
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			if err := tx.ObserveMCP(now, v.Samples); err != nil {
				return err
			}
			return tx.ReplaceProcs(ps)
		}); err != nil {
			t.Fatal(err)
		}
	}
	tc := toolCase{"get_machine", map[string]any{"machine": "local", "limit": 500, "full": true}, []string{"machines", "show", "local", "--full"}}
	got := call(t, connect(t, globals()), tc)
	var tool, cli map[string]any
	if err := json.Unmarshal(canonical(t, got.StructuredContent), &tool); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(cliJSON(t, tc.cli), &cli); err != nil {
		t.Fatal(err)
	}
	servers := func(w map[string]any) map[string]any {
		return w["data"].(map[string]any)["mcp_servers"].(map[string]any)
	}
	a, b := servers(tool), servers(cli)
	if string(canonical(t, a["trends"])) != string(canonical(t, b["trends"])) {
		t.Fatalf("trend parity: %+v %+v", a, b)
	}
	trend := a["trends"].([]any)[0].(map[string]any)
	if trend["state"] != "leaking" || trend["started_count"] != float64(2) || trend["ended_count"] != float64(0) || trend["net_growth"] != float64(2) || trend["rate_per_hour"] != .75 || trend["first_sample"] != at.Format(time.RFC3339) || trend["last_sample"] != at.Add(160*time.Minute).Format(time.RFC3339) {
		t.Fatal(trend)
	}
	for _, field := range []string{"commands", "pileups"} {
		row := a[field].([]any)[0].(map[string]any)
		if string(canonical(t, row["trends"])) != string(canonical(t, a["trends"])) {
			t.Fatal(row)
		}
	}
}

func TestMachineWholeHostCLIParity(t *testing.T) {
	st, id := fixture(t)
	t.Setenv("PATH", t.TempDir())
	at := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		s, err := tx.Session(id)
		if err != nil {
			return err
		}
		s.PID, s.ProcStart = 42, at
		if _, err := tx.UpsertSession(s); err != nil {
			return err
		}
		if err := tx.InsertSample(model.MachineSample{TS: at, MemUsed: 8 << 30, MemCompressed: 2 << 30, PressureLevel: "critical"}); err != nil {
			return err
		}
		return tx.ReplaceProcs([]model.Proc{{PID: 42, Exe: "/bin/claude", StartTime: at, RSS: 10, Footprint: 30, MemoryMeasure: "physical_footprint", CPUPercent: 10}, {PID: 43, PPID: 42, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, StartTime: at.Add(time.Second), Exe: "/bin/worker", RSS: 20, Footprint: 50, MemoryMeasure: "physical_footprint", CPUPercent: 70}, {PID: 45, PPID: 42, Cmdline: "node /opt/codegraph/dist/index.js", Argv: []string{"node", "/opt/codegraph/dist/index.js"}, StartTime: at.Add(2 * time.Second), Exe: "/bin/worker", RSS: 20}, {PID: 44, Exe: "/Applications/Browser.app/Contents/MacOS/Browser", RSS: 90, MemoryMeasure: "rss", CPUPercent: 5}})
	}); err != nil {
		t.Fatal(err)
	}
	tc := toolCase{"get_machine", map[string]any{"machine": "local"}, []string{"machines", "show", "local"}}
	got := call(t, connect(t, globals()), tc)
	cli := cliJSON(t, tc.cli)
	var wire map[string]any
	if err := json.Unmarshal(cli, &wire); err != nil {
		t.Fatal(err)
	}
	if string(canonical(t, got.StructuredContent)) != string(canonical(t, wire)) {
		t.Fatalf("MCP differs from CLI: %+v %s", got.StructuredContent, cli)
	}
	data := wire["data"].(map[string]any)
	servers, ok := data["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatal("missing MCP servers", data)
	}
	pileups := servers["pileups"].([]any)
	if len(pileups) != 1 || pileups[0].(map[string]any)["count"] != float64(2) {
		t.Fatal(servers)
	}
	commands := servers["commands"].([]any)
	if len(commands) != 1 || commands[0].(map[string]any)["host_count"] != float64(1) {
		t.Fatal(servers)
	}
	cpu := data["top_cpu"].([]any)
	if len(cpu) != 3 || cpu[0].(map[string]any)["name"] != "worker" {
		t.Fatal(data)
	}
	p := cpu[0].(map[string]any)["processes"].([]any)[0].(map[string]any)
	if p["memory_bytes"] != float64(50) || p["attribution"].(map[string]any)["session_id"] != float64(id) {
		t.Fatal(p)
	}
	if data["sample"].(map[string]any)["pressure_level"] != "critical" {
		t.Fatal(data)
	}
}
