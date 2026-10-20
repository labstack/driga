package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

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
		return tx.ReplaceProcs([]model.Proc{{PID: 42, Exe: "/bin/claude", StartTime: at, RSS: 10, Footprint: 30, MemoryMeasure: "physical_footprint", CPUPercent: 10}, {PID: 43, PPID: 42, Cmdline: "node /opt/codegraph/dist/index.js", StartTime: at.Add(time.Second), Exe: "/bin/worker", RSS: 20, Footprint: 50, MemoryMeasure: "physical_footprint", CPUPercent: 70}, {PID: 45, PPID: 42, Cmdline: "node /opt/codegraph/dist/index.js", StartTime: at.Add(2 * time.Second), Exe: "/bin/worker", RSS: 20}, {PID: 44, Exe: "/Applications/Browser.app/Contents/MacOS/Browser", RSS: 90, MemoryMeasure: "rss", CPUPercent: 5}})
	}); err != nil {
		t.Fatal(err)
	}
	tc := toolCase{"get_machine", map[string]any{"machine": "local"}, []string{"machine", "--machine", "local"}}
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
