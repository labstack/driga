package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestMCPTrendCollectionAndPressure(t *testing.T) {
	d, st, p, _ := setup(t)
	t.Setenv("PATH", t.TempDir())
	d.Adapters = nil
	at := time.Date(2026, 10, 20, 8, 12, 0, 0, time.UTC)
	now := at
	d.Now = func() time.Time { return now }
	procs := []model.Proc{{PID: 100, Cmdline: "codex app-server", StartTime: at.Add(-time.Hour)}}
	for i := range 3 {
		now = at.Add(time.Duration(i) * 80 * time.Minute)
		procs = append(procs, model.Proc{PID: int32(i + 1), PPID: 100, Cmdline: "java -cp /opt/maestro/lib/* maestro.cli.AppKt mcp", StartTime: now, RSS: 100})
		p.procs = procs
		pass(t, d, st, Plan{Procs: true})
		if i == 1 {
			d.State = NewState()
		} // durable baseline survives restart
	}
	read := func() []derive.MCPTrend {
		t.Helper()
		b := derive.NewMCPTrendBuilder()
		if err := st.ScanMCPHistory(now.Add(-30*24*time.Hour), now, func(s model.MCPObservation) error { b.Add(s); return nil }); err != nil {
			t.Fatal(err)
		}
		return b.Trends()
	}
	got := read()
	if len(got) != 1 || got[0].State != "leaking" || got[0].StartedCount != 2 || got[0].RatePerHour != .75 {
		t.Fatal(got)
	}
	// Machine-only and failed process passes cannot manufacture observations.
	now = now.Add(time.Minute)
	pass(t, d, st, Plan{Machine: true})
	p.procErr = errors.New("unavailable")
	pass(t, d, st, Plan{Procs: true})
	if !read()[0].LastSample.Equal(got[0].LastSample) {
		t.Fatal(read())
	}
	for range 2 {
		now = now.Add(time.Minute)
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			return memoryPressureInbox(tx, model.MachineSample{TS: now, PressureLevel: "critical"}, now, procs)
		}); err != nil {
			t.Fatal(err)
		}
	}
	var summary string
	for _, item := range inbox(t, st) {
		if item.Kind == "memory_pressure" {
			summary = item.Summary
		}
	}
	for _, want := range []string{"codex app-server PID 100", "1 maestro mcp every 80 min", "none closed since", "observed through"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("missing %q: %s", want, summary)
		}
	}
	// A real exit ends the mechanism, even when the count remains a pile-up.
	p.procErr = nil
	p.procs = procs[:len(procs)-1]
	pass(t, d, st, Plan{Procs: true})
	if read()[0].State != "churning" {
		t.Fatal(read())
	}
}

func TestMCPPressureHistoryBoundedTo24Hours(t *testing.T) {
	_, st, _, _ := setup(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	// The same currently visible host/command has an old observed-growth history.
	procs := []model.Proc{{PID: 100, Cmdline: "claude", StartTime: now.Add(-72 * time.Hour)}}
	for i := 0; i < 3; i++ {
		at := now.Add(-48*time.Hour + time.Duration(i)*time.Hour)
		procs = append(procs, model.Proc{PID: int32(i + 1), PPID: 100, Cmdline: "node /opt/server.js", StartTime: at, RSS: 1 << 30})
		observations := derive.MCPServers(procs, nil, at).Samples
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.ObserveMCP(at, observations) }); err != nil {
			t.Fatal(err)
		}
	}
	// Ensure the fixture would trigger growth advice without the 24h bound.
	baseline := derive.NewMCPTrendBuilder()
	if err := st.ScanMCPHistory(now.Add(-30*24*time.Hour), now, func(row model.MCPObservation) error { baseline.Add(row); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(baseline.Trends()) != 1 || baseline.Trends()[0].State != "leaking" {
		t.Fatal(baseline.Trends())
	}
	for i := 0; i < 2; i++ {
		at := now.Add(time.Duration(i) * time.Minute)
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			return memoryPressureInbox(tx, model.MachineSample{TS: at, PressureLevel: "critical"}, at, procs)
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range inbox(t, st) {
		if item.Kind == "memory_pressure" && strings.Contains(item.Summary, "Largest observed growth") {
			t.Fatal("alert read history older than 24h:", item.Summary)
		}
	}
}
