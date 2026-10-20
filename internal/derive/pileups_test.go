package derive

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestMCPServersRecordedProcessTable(t *testing.T) {
	b, err := os.ReadFile("testdata/mcp_processes.json")
	if err != nil {
		t.Fatal(err)
	}
	var ps []model.Proc
	if err := json.Unmarshal(b, &ps); err != nil {
		t.Fatal(err)
	}
	sigs, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	got := MCPServers(ps, sigs, now)
	if len(got.Pileups) != 3 {
		t.Fatalf("pileups: %+v", got)
	}
	first := got.Pileups[0]
	if first.Name != "maestro mcp" || first.Count != 2 || first.Memory != 4<<30 || first.MemoryMeasure != "physical_footprint" || first.Host.PID != 10 || first.Host.Name != "codex app-server" || first.Host.Age != 31*time.Hour || first.Host.Cwd != "/projects/fixture" {
		t.Fatalf("largest: %+v", first)
	}
	if !first.OldestStart.Equal(now.Add(-30*time.Hour)) || !first.NewestStart.Equal(now.Add(-time.Hour)) {
		t.Fatal(first)
	}
	if !strings.Contains(first.Advice, "codex app-server") || !strings.Contains(first.Advice, "PID 10") || !strings.Contains(first.Advice, "Restart") || !strings.Contains(first.Advice, "per project") {
		t.Fatal(first.Advice)
	}
	if got.Pileups[1].Host.PID != 20 || got.Pileups[1].Name != "codegraph mcp" || got.Pileups[1].Count != 2 {
		t.Fatal(got.Pileups)
	}
	if got.Pileups[2].Count != 2 || got.Pileups[2].MemoryMeasure != "mixed" {
		t.Fatal(got.Pileups)
	}
	if len(got.Commands) != 4 {
		t.Fatalf("rollups: %+v", got.Commands)
	}
	maestro := got.Commands[0]
	if maestro.Count != 3 || maestro.HostCount != 2 || maestro.Memory != 6<<30 || maestro.PileupCount != 1 {
		t.Fatal(maestro)
	}
	for _, p := range got.Pileups {
		if p.Host.PID == 30 || p.Host.PID == 40 {
			t.Fatal("single instance per host became a pile-up", p)
		}
	}
	for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
		ps[i], ps[j] = ps[j], ps[i]
	}
	if reversed := MCPServers(ps, sigs, now); !reflect.DeepEqual(got, reversed) {
		t.Fatalf("unstable order: %+v", reversed)
	}
}

func TestMCPCommandNormalization(t *testing.T) {
	sigs, _ := signatures.Load(nil)
	for _, tc := range []struct {
		a, b  string
		equal bool
	}{
		{"node /opt/server/mcp.js --port 41001 --socket /private/tmp/run-a/sock", "node /opt/server/mcp.js --port 52002 --socket /tmp/run-b/sock", true},
		{"node /opt/server/mcp.js --listen localhost:41001", "node /opt/server/mcp.js --listen localhost:52002", true},
		{"node /opt/server/mcp.js --endpoint http://127.0.0.1:41001/mcp --tmp=/var/folders/aa/abcd/T/run-a", "node /opt/server/mcp.js --endpoint http://127.0.0.1:52002/mcp --tmp=/var/folders/bb/efgh/T/run-b", true},
		{"node /opt/server/mcp.js --port=41001 --project /projects/one", "node /opt/server/mcp.js --port=52002 --project /projects/two", false},
		{"node /opt/server/mcp.js --project /projects/tmp/one", "node /opt/server/mcp.js --project /projects/tmp/two", false},
		{"node /opt/server/mcp.js --workers 2", "node /opt/server/mcp.js --workers 3", false},
		{`node /opt/server/mcp.js --project "/projects/two words"`, `node /opt/server/mcp.js --project /projects/two words`, false},
		{"node /opt/one/mcp.js", "node /opt/two/mcp.js", false},
		{"node /tmp/build-a/mcp.js --port 41001", "node /tmp/build-b/mcp.js --port 41002", true},
		{"/tmp/run-a/node /opt/server/mcp.js", "/tmp/run-b/python /opt/server/mcp.js", false},
		{"/tmp/run-a/node /opt/server/mcp.js", "/tmp/run-b/node /opt/server/mcp.js", true},
	} {
		ps := []model.Proc{{PID: 1, Cmdline: "claude"}, {PID: 2, PPID: 1, Cmdline: tc.a}, {PID: 3, PPID: 1, Cmdline: tc.b}}
		got := MCPServers(ps, sigs, time.Now())
		if (len(got.Pileups) == 1) != tc.equal {
			t.Errorf("%q / %q: %+v", tc.a, tc.b, got)
		}
	}
}

func TestMCPReportedScaleWithoutThresholds(t *testing.T) {
	var ps []model.Proc
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	pid := int32(1000)
	for host := range 25 {
		hostPID := int32(host + 1)
		cmd, count, child, memory := "codex app-server", 19, "java -cp /opt/maestro/lib/* maestro.cli.AppKt mcp", uint64(200_000_000)
		if host == 0 {
			count = 24
		} else if host == 7 || host == 8 {
			count = 18
		} else if host >= 9 {
			cmd, count, child, memory = "claude", 23, "node /opt/codegraph/dist/index.js", 100_000_000
		}
		ps = append(ps, model.Proc{PID: hostPID, Cmdline: cmd, StartTime: at.Add(-31 * time.Hour)})
		for i := range count {
			pid++
			ps = append(ps, model.Proc{PID: pid, PPID: hostPID, Cmdline: child, RSS: memory, StartTime: at.Add(time.Duration(i-count) * time.Hour)})
		}
	}
	got := MCPServers(ps, nil, at)
	if len(got.Pileups) != 25 || len(got.Commands) != 2 {
		t.Fatal(got)
	}
	for _, c := range got.Commands {
		want, hosts := 174, 9
		if c.Name == "codegraph mcp" {
			want, hosts = 368, 16
		}
		if c.Count != want || c.HostCount != hosts {
			t.Errorf("%s: %+v", fmt.Sprint(c.Name), c)
		}
	}
}

func TestMCPAncestrySafety(t *testing.T) {
	sigs, _ := signatures.Load(nil)
	at := time.Unix(1000, 0).UTC()
	ps := []model.Proc{
		{PID: 1, Cmdline: "claude", StartTime: at},
		{PID: 2, PPID: 1, Cmdline: "codex app-server", StartTime: at},
		{PID: 3, PPID: 2, Cmdline: "sh wrapper", StartTime: at},
		{PID: 4, PPID: 3, Cmdline: "node /opt/mcp/server.js", StartTime: at},
		{PID: 5, PPID: 3, Cmdline: "node /opt/mcp/server.js", StartTime: at},
		{PID: 6, PPID: 2, Cmdline: "node /opt/mcp/server.js", StartTime: at.Add(-time.Second)}, // PID reuse
		{PID: 7, PPID: 2, Cmdline: "node /opt/mcp/server.js", Zombie: true},
		{PID: 8, PPID: 9, Cmdline: "node /opt/mcp/server.js"}, {PID: 9, PPID: 8, Cmdline: "node /opt/mcp/server.js"},
		{PID: 10, PPID: 2, Cmdline: "sleep 60"}, {PID: 11, PPID: 2, Cmdline: "sleep 61"},
	}
	got := MCPServers(ps, sigs, at.Add(time.Hour))
	if len(got.Pileups) != 1 || got.Pileups[0].Host.PID != 2 || got.Pileups[0].Count != 2 || len(got.Commands) != 4 {
		t.Fatal(got)
	}
	for _, cmd := range []string{"claude", "codex", "codex app-server", "agy", "copilot", "/home/fixture/.codex/packages/app-server-daemon/1/bin/codex app-server", "/home/fixture/.local/share/claude/versions/2.1.304"} {
		ps[0].Cmdline = cmd
		got := MCPServers([]model.Proc{ps[0], {PID: 12, PPID: 1, Cmdline: "node /opt/mcp/server.js"}, {PID: 13, PPID: 1, Cmdline: "node /opt/mcp/server.js"}}, sigs, at)
		if len(got.Pileups) != 1 {
			t.Errorf("host %q: %+v", cmd, got)
		}
	}
}

func TestDuplicateChildCommandsNeedNoMCPMarker(t *testing.T) {
	ps := []model.Proc{{PID: 1, Cmdline: "claude"}, {PID: 2, PPID: 1, Cmdline: "node /opt/tools/index.js", RSS: 1}, {PID: 3, PPID: 1, Cmdline: "node /opt/tools/index.js", RSS: 1}}
	got := MCPServers(ps, nil, time.Now())
	if len(got.Pileups) != 1 || got.Pileups[0].Count != 2 {
		t.Fatalf("all duplicate child commands must be reported: %+v", got)
	}
}

// npm ships native builds as per-platform packages; the label and identity
// drop the -<os>-<arch> suffix, so one server on two platforms is one row.
func TestMCPLabelDropsNPMPlatformSuffix(t *testing.T) {
	ps := []model.Proc{
		{PID: 1, Cmdline: "claude"},
		{PID: 4, PPID: 1, Cmdline: "/x/node_modules/@colbymchenry/codegraph/node_modules/@colbymchenry/codegraph-darwin-arm64/node --liftoff-only /x/node_modules/@colbymchenry/codegraph/node_modules/@colbymchenry/codegraph-darwin-arm64/lib/dist/bin/codegraph.js serve --mcp", RSS: 1},
		{PID: 10, Cmdline: "claude"},
		{PID: 11, PPID: 10, Cmdline: "/y/node_modules/@colbymchenry/codegraph/node_modules/@colbymchenry/codegraph-linux-x64/node --liftoff-only /y/node_modules/@colbymchenry/codegraph/node_modules/@colbymchenry/codegraph-linux-x64/lib/dist/bin/codegraph.js serve --mcp", RSS: 1},
	}
	got := MCPServers(ps, nil, time.Now()).Commands
	if len(got) != 1 || got[0].Name != "codegraph" || got[0].Count != 2 || got[0].HostCount != 2 {
		t.Fatalf("%+v", got)
	}
}
