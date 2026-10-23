package derive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/kballard/go-shellquote"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestMCPPlatformArgv(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/mixed/argv/procs.json")
	if err != nil {
		t.Fatal(err)
	}
	var procs []model.Proc
	if err := json.Unmarshal(data, &procs); err != nil {
		t.Fatal(err)
	}
	sigs, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range procs[1:] {
		d := describeMCP(p, sigs)
		if d.key != "npm:@harbor/scribe" || len(d.args) != 2 {
			t.Fatalf("lost platform argv: %+v", d)
		}
	}
	if normalizedMCPCommand(procs[2]) == normalizedMCPCommand(model.Proc{Exe: procs[2].Exe}) {
		t.Fatal("lost arguments")
	}
}

func fixtureArgv(command string) []string {
	args, _ := shellquote.Split(command)
	return args
}

func TestMCPArgvBoundaries(t *testing.T) {
	a := model.Proc{Argv: []string{"node", "/opt/node_modules/tool/cli.js", "--project", "a b"}}
	b := model.Proc{Argv: []string{"node", "/opt/node_modules/tool/cli.js", "--project", "a", "b"}}
	if normalizedMCPCommand(a) == normalizedMCPCommand(b) || describeMCP(a, nil).groupKey() == describeMCP(b, nil).groupKey() {
		t.Fatal("merged distinct argv")
	}
	p := model.Proc{Argv: []string{`C:\Program Files\nodejs\node.exe`, `C:\Users\Example\node_modules\tool\cli.js`, "--project", `C:\Users\Example\My Project`}}
	d := describeMCP(p, nil)
	if d.key != "npm:tool" || len(d.args) != 2 || d.args[1] != p.Argv[3] {
		t.Fatalf("changed Windows argument: %+v", d)
	}
}
