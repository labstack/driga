package derive

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestMCPReportedCommands(t *testing.T) {
	b, err := os.ReadFile("testdata/mcp_reported_commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var ps []model.Proc
	if err := json.Unmarshal(b, &ps); err != nil {
		t.Fatal(err)
	}
	got := MCPServers(ps, nil, time.Unix(1000, 0).UTC())
	wants := map[string]struct {
		count, hosts, duplicates int
		memory                   uint64
		launcher                 bool
	}{
		"codegraph":                {3, 2, 1, 320, true},
		"claude 2.1.304 mcp serve": {1, 1, 0, 88, false},
		"firecrawl-mcp":            {1, 1, 0, 100, true},
		"playwright-mcp":           {2, 2, 0, 140, true},
		"ChatGPT · cua node_repl":  {2, 1, 1, 66, false},
		"ChatGPT · cua-repl.mjs":   {1, 1, 0, 16, false},
		"sample-app/server mcp":    {1, 1, 0, 166, false},
		"maestro mcp":              {1, 1, 0, 165, false},
	}
	if len(got.Commands) != len(wants) {
		t.Errorf("got %d rows, want %d: %+v", len(got.Commands), len(wants), got.Commands)
	}
	for i, c := range got.Commands {
		want, ok := wants[c.Name]
		if !ok {
			t.Errorf("unexpected label %q", c.Name)
			continue
		}
		delete(wants, c.Name)
		if c.Count != want.count || c.HostCount != want.hosts || c.Memory != want.memory {
			t.Errorf("%s: %+v", c.Name, c)
		}
		encoded, _ := json.Marshal(c)
		var evidence map[string]any
		_ = json.Unmarshal(encoded, &evidence)
		if evidence["duplicate_count"] != float64(want.duplicates) || evidence["includes_launcher"] != want.launcher {
			t.Errorf("metadata: %s", encoded)
		}
		if c.Name == "playwright-mcp" {
			for _, install := range []string{"/opt/homebrew/bin/node", "/Users/example/.local/share/mise/installs/node/24.1.0/bin/node"} {
				if !strings.Contains(string(encoded), install) {
					t.Errorf("missing install %s: %s", install, encoded)
				}
			}
		}
		if i > 0 && got.Commands[i-1].Memory < c.Memory {
			t.Fatal("not sorted by memory")
		}
	}
	if len(wants) > 0 {
		t.Errorf("missing labels: %+v", wants)
	}
	// Every original command, including collapsed launchers, remains in JSON.
	encoded, _ := json.Marshal(got)
	var evidence any
	_ = json.Unmarshal(encoded, &evidence)
	for _, p := range ps[2:] {
		quoted, _ := json.Marshal(p.Cmdline)
		if !strings.Contains(string(encoded), string(quoted)) {
			t.Errorf("lost command %q", p.Cmdline)
		}
	}
	for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
		ps[i], ps[j] = ps[j], ps[i]
	}
	if reversed := MCPServers(ps, nil, time.Unix(1000, 0).UTC()); !reflect.DeepEqual(got, reversed) {
		t.Fatal("input order changed report")
	}
}

func TestMCPLauncherEvidence(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	for _, tc := range []struct {
		name, launcher, child string
		extra, older          bool
		count                 int
	}{
		{"npx", "npx -y firecrawl-mcp", "node /cache/node_modules/.bin/firecrawl-mcp", false, false, 1},
		{"node shim", "node /usr/local/bin/widget --stdio", "node /usr/local/lib/node_modules/widget/dist/cli.js --stdio", false, false, 1},
		{"different args", "node /usr/local/bin/widget --project /one", "node /usr/local/lib/node_modules/widget/dist/cli.js --project /two", false, false, 2},
		{"different launch ports", "node exec widget --port 1000", "node /cache/node_modules/.bin/widget --port 2000", false, false, 2},
		{"different target", "node exec firecrawl-mcp", "node /cache/node_modules/.bin/other-mcp", false, false, 2},
		{"multiple children", "node exec firecrawl-mcp", "node /cache/node_modules/.bin/firecrawl-mcp", true, false, 3},
		{"PID reuse", "node exec firecrawl-mcp", "node /cache/node_modules/.bin/firecrawl-mcp", false, true, 1},
		{"server workers", "node /cache/node_modules/.bin/firecrawl-mcp", "node /cache/node_modules/.bin/firecrawl-mcp", false, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := []model.Proc{{PID: 1, Cmdline: "claude", Argv: []string{"claude"}, StartTime: at.Add(-time.Hour)}, {PID: 2, PPID: 1, Cmdline: tc.launcher, Argv: fixtureArgv(tc.launcher), StartTime: at, RSS: 10}, {PID: 3, PPID: 2, Cmdline: tc.child, Argv: fixtureArgv(tc.child), StartTime: at.Add(time.Second), RSS: 20}}
			if tc.older {
				ps[2].StartTime = at.Add(-time.Second)
			}
			if tc.extra {
				ps = append(ps, model.Proc{PID: 4, PPID: 2, Cmdline: "sleep 60", Argv: []string{"sleep", "60"}, StartTime: at.Add(time.Second), RSS: 5})
			}
			got := MCPServers(ps, nil, at.Add(time.Hour))
			count := 0
			for _, c := range got.Commands {
				count += c.Count
			}
			if count != tc.count {
				t.Fatalf("got %d servers, want %d: %+v", count, tc.count, got)
			}
			if tc.count == 1 && !tc.older && (len(got.Pileups) != 0 || len(got.Commands) != 1 || got.Commands[0].Memory != 30) {
				t.Fatal(got)
			}
		})
	}
}

func TestMCPPackageGroupingPreservesArgumentsAndScopes(t *testing.T) {
	for _, pair := range [][2]string{
		{"node /a/node_modules/@north/tool/cli.js", "node /b/node_modules/@south/tool/cli.js"},
		{"/a/bin/node /a/node_modules/widget/cli.js --project /one", "/b/bin/node /b/node_modules/widget/cli.js --project /two"},
		{"node /a/tool.js", "node /b/tool.js"},
		{"/Applications/Editor.app/Contents/Resources/server --project /one", "/Applications/Editor.app/Contents/Resources/server --project /two"},
	} {
		got := MCPServers([]model.Proc{{PID: 1, Cmdline: "claude", Argv: []string{"claude"}}, {PID: 2, PPID: 1, Cmdline: pair[0], Argv: fixtureArgv(pair[0])}, {PID: 3, PPID: 1, Cmdline: pair[1], Argv: fixtureArgv(pair[1])}}, nil, time.Time{})
		if len(got.Commands) != 2 {
			t.Fatalf("merged distinct servers: %+v", got)
		}
	}
}

func TestMCPLauncherReportedScale(t *testing.T) {
	var ps []model.Proc
	ps = append(ps, model.Proc{PID: 1, Cmdline: "codex app-server", Argv: []string{"codex", "app-server"}})
	for i := range 85 {
		pid := int32(10 + i*2)
		ps = append(ps,
			model.Proc{PID: pid, PPID: 1, Cmdline: "node exec firecrawl-mcp", Argv: []string{"node", "exec", "firecrawl-mcp"}, Footprint: 15, MemoryMeasure: "physical_footprint"},
			model.Proc{PID: pid + 1, PPID: pid, Cmdline: "node /Users/example/.npm/_npx/12b05d58670d8359/node_modules/.bin/firecrawl-mcp", Argv: []string{"node", "/Users/example/.npm/_npx/12b05d58670d8359/node_modules/.bin/firecrawl-mcp"}, Footprint: 85, MemoryMeasure: "physical_footprint"})
	}
	for i := range 166 {
		pid := int32(1000 + i*2)
		ps = append(ps,
			model.Proc{PID: pid, PPID: 1, Cmdline: "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl", Argv: []string{"/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"}, RSS: 1},
			model.Proc{PID: pid + 1, PPID: pid, Cmdline: "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl", Argv: []string{"/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"}, RSS: 1})
	}
	got := MCPServers(ps, nil, time.Time{})
	if len(got.Commands) != 2 || len(got.Pileups) != 2 {
		t.Fatal(got)
	}
	c := got.Commands[0]
	if c.Name != "firecrawl-mcp" || c.Count != 85 || c.Memory != 8500 || c.DuplicateCount != 84 || !c.IncludesLauncher {
		t.Fatal(c)
	}
	c = got.Commands[1]
	if c.Name != "ChatGPT · cua node_repl" || c.Count != 332 || c.DuplicateCount != 331 || c.IncludesLauncher {
		t.Fatal(c)
	}
}

func TestMCPUnseenPackageInstallLayouts(t *testing.T) {
	// Held-out names/layouts pin structural rules, without another signature.
	ps := []model.Proc{
		{PID: 1, Cmdline: "claude", Argv: []string{"claude"}},
		{PID: 2, PPID: 1, Cmdline: "/usr/local/bin/node /usr/local/lib/node_modules/@harbor/scribe/dist/cli.js --stdio", Argv: []string{"/usr/local/bin/node", "/usr/local/lib/node_modules/@harbor/scribe/dist/cli.js", "--stdio"}, RSS: 1},
		{PID: 3, PPID: 1, Cmdline: "/home/test/.local/share/mise/installs/node/22/bin/node /home/test/.local/share/mise/installs/node/22/lib/node_modules/@harbor/scribe/dist/cli.js --stdio", Argv: []string{"/home/test/.local/share/mise/installs/node/22/bin/node", "/home/test/.local/share/mise/installs/node/22/lib/node_modules/@harbor/scribe/dist/cli.js", "--stdio"}, RSS: 2},
		{PID: 4, PPID: 1, Cmdline: "/Applications/Research Desk.app/Contents/Resources/search_node/bin/node_repl", Argv: []string{"/Applications/Research", "Desk.app/Contents/Resources/search_node/bin/node_repl"}, RSS: 3},
	}
	// Command strings with spaces require the quoting supplied by the OS.
	ps[3].Cmdline = `"/Applications/Research Desk.app/Contents/Resources/search_node/bin/node_repl"`
	ps[3].Argv = []string{"/Applications/Research Desk.app/Contents/Resources/search_node/bin/node_repl"}
	got := MCPServers(ps, nil, time.Time{})
	if len(got.Commands) != 2 {
		t.Fatal(got)
	}
	for _, c := range got.Commands {
		switch c.Name {
		case "scribe":
			if c.Count != 2 || c.Memory != 3 || c.DuplicateCount != 1 {
				t.Fatal(c)
			}
		case "Research Desk · search node_repl":
			if c.Count != 1 {
				t.Fatal(c)
			}
		default:
			t.Fatal(c)
		}
	}
}

func TestMCPLauncherPackageScopesRemainDistinct(t *testing.T) {
	got := MCPServers([]model.Proc{
		{PID: 1, Cmdline: "claude", Argv: []string{"claude"}},
		{PID: 2, PPID: 1, Cmdline: "node exec @north/tool", Argv: []string{"node", "exec", "@north/tool"}},
		{PID: 3, PPID: 2, Cmdline: "node /a/node_modules/.bin/tool", Argv: []string{"node", "/a/node_modules/.bin/tool"}},
		{PID: 4, PPID: 1, Cmdline: "node exec @south/tool", Argv: []string{"node", "exec", "@south/tool"}},
		{PID: 5, PPID: 4, Cmdline: "node /b/node_modules/.bin/tool", Argv: []string{"node", "/b/node_modules/.bin/tool"}},
	}, nil, time.Time{})
	if len(got.Commands) != 2 || len(got.Pileups) != 0 {
		t.Fatalf("merged different launcher packages: %+v", got)
	}
}

func TestMCPPackageAliasAlsoGroupsUnwrappedBins(t *testing.T) {
	got := MCPServers([]model.Proc{
		{PID: 1, Cmdline: "claude", Argv: []string{"claude"}},
		{PID: 2, PPID: 1, Cmdline: "node exec @harbor/scribe", Argv: []string{"node", "exec", "@harbor/scribe"}},
		{PID: 3, PPID: 2, Cmdline: "node /a/node_modules/.bin/scribe", Argv: []string{"node", "/a/node_modules/.bin/scribe"}},
		{PID: 4, PPID: 1, Cmdline: "/another/bin/node /b/node_modules/.bin/scribe", Argv: []string{"/another/bin/node", "/b/node_modules/.bin/scribe"}},
	}, nil, time.Time{})
	if len(got.Commands) != 1 || got.Commands[0].Count != 2 {
		t.Fatal(got)
	}
}
