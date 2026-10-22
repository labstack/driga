package view

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/fixturegen"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
)

func machineHistoryFixture(t *testing.T) *store.Store {
	t.Helper()
	home := t.TempDir()
	ps := []model.Proc{{PID: 1000, Cmdline: "claude", StartTime: testNow.Add(-72 * time.Hour)}}
	for n := range 60 {
		for member := range 3 {
			ps = append(ps, model.Proc{PID: int32(n*3 + member + 1), PPID: 1000, Cmdline: fmt.Sprintf("node /opt/server-%02d/mcp.js", n), StartTime: testNow.Add(-60 * time.Hour), RSS: uint64(n+1) << 20})
		}
	}
	if err := fixturegen.Write(home, fixturegen.Scenario{Now: testNow, Procs: ps}); err != nil {
		t.Fatal(err)
	}
	plat, err := platform.LoadFake(home)
	if err != nil {
		t.Fatal(err)
	}
	ps, err = plat.Procs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return fixture(t, func(tx *store.Tx) error {
		if err := tx.ReplaceProcs(ps); err != nil {
			return err
		}
		for _, at := range []time.Time{testNow.Add(-48 * time.Hour), testNow.Add(-30 * time.Minute), testNow} {
			samples := derive.MCPServers(ps, nil, at).Samples
			if err := tx.ObserveMCP(at, samples); err != nil {
				return err
			}
		}
		return nil
	})
}

func TestMachineDefaultBoundsAndUniqueReports(t *testing.T) {
	st := machineHistoryFixture(t)
	v, err := Machine(st, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.MCPServers.Trends) != 10 || len(v.MCPServers.Commands) != 10 || len(v.MCPServers.Pileups) != 10 {
		t.Errorf("unbounded machine report: trends=%d commands=%d pileups=%d", len(v.MCPServers.Trends), len(v.MCPServers.Commands), len(v.MCPServers.Pileups))
	}
	if v.AgentShare.MCPServers != 180 || v.AgentShare.Processes != 181 || v.AgentShare.Memory != 5490<<20 {
		t.Fatalf("bounded lists changed totals: %+v", v.AgentShare)
	}
	for _, trend := range v.MCPServers.Trends {
		if trend.FirstSample.Before(testNow.Add(-24 * time.Hour)) {
			t.Error("default read included old trend evidence")
			break
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Machines []struct {
			MachineID string `json:"machine_id"`
		} `json:"machines"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	for _, m := range wire.Machines {
		if m.MachineID == v.MachineID {
			t.Error("primary report duplicated in machines")
		}
	}
	full, err := Machine(st, Filter{Full: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	fullJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	var compactText, fullText bytes.Buffer
	if err := Render(&compactText, v, RenderOpts{NoColor: true}); err != nil {
		t.Fatal(err)
	}
	if err := Render(&fullText, full, RenderOpts{NoColor: true}); err != nil {
		t.Fatal(err)
	}
	compactLines, fullLines := strings.Count(compactText.String(), "\n"), strings.Count(fullText.String(), "\n")
	if compactLines >= fullLines || len(b) >= len(fullJSON) {
		t.Fatal("default output is not compact")
	}
	t.Logf("default/full: text %d/%d lines; JSON %d/%d bytes", compactLines, fullLines, len(b), len(fullJSON))
}

type noMachineHistory struct{ Reader }

func TestSummaryDoesNotBuildMachineHistory(t *testing.T) {
	st := machineHistoryFixture(t)
	if _, err := summaryLocal(noMachineHistory{st}, Filter{}, testNow); err != nil {
		t.Fatal(err)
	}
}

func TestMachineFullRetainsEvidence(t *testing.T) {
	st := machineHistoryFixture(t)
	v, err := Machine(st, Filter{Full: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.MCPServers.Trends) != 60 || len(v.MCPServers.Commands) != 60 || v.Truncated || !v.Full {
		t.Fatalf("full report lost evidence: %+v", v)
	}
	for _, trend := range v.MCPServers.Trends {
		if !trend.FirstSample.Equal(testNow.Add(-48 * time.Hour)) {
			t.Fatal("full window lost", trend)
		}
	}
}

func (noMachineHistory) ScanMCPHistory(time.Time, time.Time, func(model.MCPObservation) error) error {
	return fmt.Errorf("summary read recent machine history")
}
