package collector

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestInternalSignatureIgnored(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	start := d.Now().Add(-time.Hour)
	p.procs = []model.Proc{
		{PID: 42, PPID: 1, Exe: "claude", Cmdline: "claude -p --name driga-internal", Argv: []string{"claude", "-p", "--name", "driga-internal"}, StartTime: start},
		{PID: 43, PPID: 42, Exe: "npx", Cmdline: "npx some-mcp", Argv: []string{"npx", "some-mcp"}, StartTime: start},
	}
	pass(t, d, st, Plan{Procs: true})
	if ss := sessions(t, st); len(ss) != 0 {
		t.Fatal(ss)
	}
	oo, err := st.Orphans()
	if err != nil || len(oo) != 0 {
		t.Fatal(oo, err)
	}
	pp, err := st.Procs()
	if err != nil || len(pp) != 2 || pp[0].Role != "internal" {
		t.Fatal(pp, err)
	}
}

func TestInternalTreeNotOrphan(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	start := d.Now().Add(-time.Hour)
	p.procs = []model.Proc{
		{PID: 41, PPID: 1, Cmdline: "npx helper-mcp", Argv: []string{"npx", "helper-mcp"}, StartTime: start},
		{PID: 42, PPID: 41, Cmdline: "claude -p --name driga-internal", Argv: []string{"claude", "-p", "--name", "driga-internal"}, StartTime: start},
	}
	pass(t, d, st, Plan{Procs: true})
	oo, err := st.Orphans()
	if err != nil || len(oo) != 0 {
		t.Fatal("internal process included in candidate", oo, err)
	}
}
