package web

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestMachineMCPLeakMarker(t *testing.T) {
	// Pin the zone before the fixture starts its server goroutine (which reads
	// the clock), and restore it after that goroutine stops: cleanups run LIFO.
	original := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = original })
	s, st, _, _ := fixture(t)
	t.Setenv("PATH", t.TempDir())
	at := time.Date(2020, 1, 1, 8, 12, 0, 0, time.UTC)
	ps := []model.Proc{{PID: 100, Cmdline: "codex app-server", StartTime: at.Add(-time.Hour)}}
	for i := range 3 {
		now := at.Add(time.Duration(i) * 80 * time.Minute)
		ps = append(ps, model.Proc{PID: int32(i + 1), PPID: 100, Cmdline: "node /opt/codegraph/dist/index.js", StartTime: now, RSS: 100})
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
	w := request(s, "GET", "/machines", s.token)
	for _, want := range []string{"leaking ↑ 0.75/h since 08:12", "started 2", "ended 0", "net &#43;2", "Observed: 2020-01-01T08:12:00Z to 2020-01-01T10:52:00Z"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("missing %q: %s", want, w.Body)
		}
	}
}

func TestMachineExpandableAppsAndEscaping(t *testing.T) {
	s, st, id, _ := fixture(t)
	t.Setenv("PATH", t.TempDir())
	at := time.Now().Add(-time.Minute)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		ss, err := tx.Session(id)
		if err != nil {
			return err
		}
		ss.PID, ss.ProcStart, ss.StartedAt = 42, at, at
		if _, err := tx.UpsertSession(ss); err != nil {
			return err
		}
		return tx.ReplaceProcs([]model.Proc{{PID: 42, StartTime: at, Exe: "/Applications/Browser.app/Contents/MacOS/Browser", Footprint: 1 << 30, MemoryMeasure: "physical_footprint", CPUPercent: 50}, {PID: 43, PPID: 42, StartTime: at.Add(time.Second), Exe: "/bin/<script>", RSS: 100, MemoryMeasure: "rss"}})
	}); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/machines", s.token)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, part := range []string{"Top 10 apps by memory", "Top 10 apps by CPU", "<summary>Browser", "physical_footprint", "PID 43", "<dt>compressed</dt>", "session #", "parent_chain", "/bin/&lt;script&gt;"} {
		if !strings.Contains(w.Body.String(), part) {
			t.Fatalf("missing %q: %s", part, w.Body)
		}
	}
	if strings.Contains(w.Body.String(), "/bin/<script>") {
		t.Fatal("unescaped process executable")
	}
}

func TestMachineDisclosureSurvivesRefresh(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node JavaScript runner unavailable")
	}
	t.Setenv("PATH", t.TempDir())
	js, err := assets.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	const fixture = `
 const make=(machine,rank,app,open=false)=>({dataset:{machineApp:app},open,closest:()=>({dataset:{machineId:machine,machineRank:rank}})});
 let rows=[make("local","memory","browser",true),make("local","cpu","browser"),make("peer","memory","browser")];
 const c=DrigaConsole.createMachineController({querySelectorAll:()=>rows});
 c.capture();rows=[make("local","memory","browser"),make("local","cpu","browser"),make("peer","memory","browser")];c.restore();
 if(!rows[0].open || rows[1].open || rows[2].open) throw Error("machine/ranking expansion identity lost");
 rows[0].open=false;c.capture();rows[0].open=true;c.restore();if(rows[0].open) throw Error("closed disclosure reopened");
 `
	cmd := exec.Command(node, "-e", string(js)+fixture)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("machine disclosures: %v\n%s", err, out)
	}
}

func TestMachineMCPServers(t *testing.T) {
	s, st, _, _ := fixture(t)
	t.Setenv("PATH", t.TempDir())
	at := time.Now().Add(-31 * time.Hour)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{
			{PID: 10, Cmdline: "codex app-server", StartTime: at, Cwd: "/projects/<fixture>"},
			{PID: 11, PPID: 10, Cmdline: "java -cp /opt/maestro/lib/* maestro.cli.AppKt mcp", RSS: 2 << 30, StartTime: at.Add(time.Hour)},
			{PID: 12, PPID: 10, Cmdline: "java -cp /opt/maestro/lib/* maestro.cli.AppKt mcp", RSS: 2 << 30, StartTime: at.Add(30 * time.Hour)},
		})
	}); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/machines", s.token)
	for _, want := range []string{"MCP servers", "maestro mcp", "2 processes", "1 hosts", "codex app-server", "PID 10", "31h", "oldest", "newest", "Restart", "per project", "/projects/&lt;fixture&gt;"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("missing %q: %s", want, w.Body)
		}
	}
}

func TestMachineMCPLauncherSummaryAndEvidence(t *testing.T) {
	s, st, _, _ := fixture(t)
	t.Setenv("PATH", t.TempDir())
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{{PID: 1, Cmdline: "claude"}, {PID: 2, PPID: 1, Cmdline: "node exec firecrawl-mcp --project '<fixture>'", RSS: 10 << 20}, {PID: 3, PPID: 2, Cmdline: "node /cache/node_modules/.bin/firecrawl-mcp --project '<fixture>'", RSS: 20 << 20}})
	}); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/machines", s.token)
	for _, want := range []string{"firecrawl-mcp  1 servers · 30.0M · 1 hosts · 0 duplicates", "includes launcher", "Installs:", "node exec firecrawl-mcp", "/cache/node_modules/.bin/firecrawl-mcp", "&lt;fixture&gt;"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %q: %s", want, w.Body)
		}
	}
	if strings.Contains(w.Body.String(), "<fixture>") {
		t.Fatal("unescaped command")
	}
}
