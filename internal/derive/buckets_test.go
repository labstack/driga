package derive

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func bucketFixture() ([]model.Proc, []model.Session) {
	at := time.Unix(1000, 0).UTC()
	return []model.Proc{{PID: 10, Exe: "/bin/claude", Cmdline: "claude", Argv: []string{"claude"}, StartTime: at}, {PID: 11, PPID: 10, Exe: "/bin/node", Cmdline: "node mcp-server-x", Argv: []string{"node", "mcp-server-x"}, StartTime: at.Add(time.Second)}}, []model.Session{{ID: 1, SessionObs: model.SessionObs{PID: 10, ProcStart: at, StartedAt: at, Cwd: "/work/project"}}}
}
func TestBucketsParentChainSession(t *testing.T) {
	ps, ss := bucketFixture()
	got := PlaceProcesses(ps, ss, nil)
	for _, p := range ps {
		if got[p.PID].Bucket != BucketSession || got[p.PID].Attribution.Proof != "parent_chain" {
			t.Fatal(got)
		}
	}
}
func TestBucketsCwdStartedAfter(t *testing.T) {
	ps, ss := bucketFixture()
	ps[1].PPID = 0
	ps[1].Cwd = "/work/project/src"
	got := PlaceProcesses(ps, ss, nil)[11]
	if got.Bucket != BucketSession || got.Attribution.Proof != "cwd_started_after" {
		t.Fatal(got)
	}
}
func TestBucketsAmbiguousCwdNotAttributed(t *testing.T) {
	ps, ss := bucketFixture()
	second := ss[0]
	second.ID = 2
	second.PID = 20
	ss = append(ss, second)
	root := ps[0]
	root.PID = 20
	ps = append(ps, root)
	ps[1].PPID = 0
	ps[1].Cwd = ss[0].Cwd
	ps[1].Cmdline = "node server.js"
	ps[1].Argv = []string{"node", "server.js"}
	if got := PlaceProcesses(ps, ss, nil)[11]; got.Bucket != BucketOther || got.Attribution != nil {
		t.Fatal(got)
	}
}
func TestBucketsAgentWithoutSession(t *testing.T) {
	ps, _ := bucketFixture()
	ps[0].Cmdline = "codex app-server"
	ps[0].Argv = []string{"codex", "app-server"}
	ps[0].Exe = "/bin/codex"
	got := PlaceProcesses(ps, nil, nil)
	for _, p := range ps {
		if got[p.PID].Bucket != BucketAgent {
			t.Fatal(got)
		}
	}
}
func editorBucketFixture() []model.Proc {
	return []model.Proc{{PID: 1, Exe: "/Applications/Editor.app/Contents/MacOS/Editor Helper (Plugin)"}, {PID: 2, PPID: 1, Exe: "/bin/node", Cmdline: "node /x/node_modules/@acme/weather-mcp/dist/index.js", Argv: []string{"node", "/x/node_modules/@acme/weather-mcp/dist/index.js"}}}
}
func TestBucketsEditorExtensionHostMCP(t *testing.T) {
	got := PlaceProcesses(editorBucketFixture(), nil, nil)
	if got[1].Bucket != BucketOther || got[2].Bucket != BucketLikely || got[2].SharedHost != 1 || got[2].Bundle != "/Applications/Editor.app" {
		t.Fatal(got)
	}
}
func TestBucketsEditorWorkerIsOther(t *testing.T) {
	ps := editorBucketFixture()
	ps[1].Cmdline = "node /x/tsserver.js"
	ps[1].Argv = []string{"node", "/x/tsserver.js"}
	if got := PlaceProcesses(ps, nil, nil)[2]; got.Bucket != BucketOther {
		t.Fatal(got)
	}
}
func TestBucketsEditorLaunchedAgentIsSession(t *testing.T) {
	ps, ss := bucketFixture()
	ps[0].PPID = 1
	ps = append(ps, editorBucketFixture()[0])
	got := PlaceProcesses(ps, ss, nil)[10]
	if got.Bucket != BucketSession || got.Bundle != "/Applications/Editor.app" {
		t.Fatal(got)
	}
}
func TestBucketsRoleOnlySignatureNotAgent(t *testing.T) {
	if got := PlaceProcesses(editorBucketFixture()[:1], nil, nil)[1]; got.Bucket != BucketOther {
		t.Fatal(got)
	}
}
func TestBucketsRejectsImpossibleParent(t *testing.T) {
	ps, ss := bucketFixture()
	ps[1].StartTime = ps[0].StartTime.Add(-time.Second)
	if got := PlaceProcesses(ps, ss, nil)[11]; got.Bucket == BucketSession || got.Bucket == BucketAgent {
		t.Fatal(got)
	}
}
func TestBucketsEveryProcessOnce(t *testing.T) {
	ps, ss := bucketFixture()
	ps = append(ps, editorBucketFixture()...)
	ps = append(ps, model.Proc{PID: 3, Zombie: true}, model.Proc{PID: 4, PPID: 4}, model.Proc{PID: 5, PPID: 6}, model.Proc{PID: 6, PPID: 5})
	got := PlaceProcesses(ps, ss, nil)
	if len(got) != len(ps)-1 {
		t.Fatal(got)
	}
	for _, p := range ps {
		if p.Zombie {
			continue
		}
		g, ok := got[p.PID]
		if !ok || g.Bucket == "" || g.PID != p.PID {
			t.Fatal(got)
		}
	}
}

func TestBucketsStoredRoleDoesNotEraseSignature(t *testing.T) {
	ps := []model.Proc{{PID: 1, Cmdline: "codex app-server", Argv: []string{"codex", "app-server"}, Agent: "codex", Role: "internal"}}
	if got := PlaceProcesses(ps, nil, nil)[1]; got.Bucket != BucketAgent {
		t.Fatal(got)
	}
}
func TestBucketsMCPTokenBoundaries(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    Bucket
	}{
		{"node /x/node_modules/@fixture/weather-MCP/index.js", BucketLikely},
		{"node /x/node_modules/@fixture/mcp_weather/index.js", BucketLikely},
		{"node /x/node_modules/@fixture/mcproxy/index.js", BucketOther},
		{"node /x/mcp/tsserver.js", BucketOther},
		{"node /x/server.js --name mcp", BucketOther},
	} {
		ps := editorBucketFixture()
		ps[1].Cmdline = tc.command
		ps[1].Argv = fixtureArgv(tc.command)
		if got := PlaceProcesses(ps, nil, nil)[2]; got.Bucket != tc.want {
			t.Fatalf("%s: %+v", tc.command, got)
		}
	}
}
func TestBucketsEditorBundledMCP(t *testing.T) {
	// An MCP server shipped inside the editor's bundle is still MCP evidence.
	ps := editorBucketFixture()
	ps[1].Exe = "/Applications/Editor.app/Contents/Resources/bin/weather-mcp"
	ps[1].Cmdline = ps[1].Exe
	ps[1].Argv = []string{ps[1].Exe}
	if got := PlaceProcesses(ps, nil, nil)[2]; got.Bucket != BucketLikely || got.SharedHost != 1 {
		t.Fatal(got)
	}
}
func TestBucketsCwdNeverClaimsOtherAgentTree(t *testing.T) {
	// Another agent without a session works in the same project. It and its
	// helpers belong to that agent, not to the session that shares the cwd.
	ps, ss := bucketFixture()
	at := ps[0].StartTime
	ps = append(ps,
		model.Proc{PID: 20, Exe: "/bin/codex", Cmdline: "codex app-server", Argv: []string{"codex", "app-server"}, Cwd: "/work/project", StartTime: at.Add(time.Minute)},
		model.Proc{PID: 21, PPID: 20, Exe: "/bin/node", Cmdline: "node tool.js", Argv: []string{"node", "tool.js"}, Cwd: "/work/project", StartTime: at.Add(2 * time.Minute)},
	)
	got := PlaceProcesses(ps, ss, nil)
	for _, pid := range []int32{20, 21} {
		if got[pid].Bucket != BucketAgent || got[pid].Attribution != nil {
			t.Fatal(pid, got[pid])
		}
	}
}
