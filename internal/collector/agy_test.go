package collector

import (
	"github.com/labstack/driga/internal/model"
	"testing"
	"time"
)

func TestAgyProcessSession(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	start := d.Now().Add(-time.Hour)
	p.procs = []model.Proc{{PID: 42, Exe: "/opt/bin/agy", Cmdline: "/opt/bin/agy", Cwd: t.TempDir(), StartTime: start}}
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	ss := sessions(t, st)
	if len(ss) != 1 {
		t.Fatalf("process sessions=%+v", ss)
	}
	s := ss[0]
	if s.Adapter != "agy" || s.PID != 42 || s.Cwd != p.procs[0].Cwd || s.Label != "unknown" || s.Title != "" {
		t.Fatal(s)
	}
	if got, err := st.Meta("warn:agy:unsupported_version"); err != nil || got == "" {
		t.Fatal(got, err)
	}
	pass(t, d, st, Plan{Procs: true})
	if got := sessions(t, st); len(got) != 1 || got[0].ID != s.ID {
		t.Fatal(got)
	}
	p.dead = true
	// A cached process sample must not resurrect a process whose liveness
	// check has already failed on a pass without a new process scan.
	pass(t, d, st, Plan{})
	if got := sessions(t, st)[0]; got.PID != 0 || got.EndedAt == nil || got.Label != "dead" {
		t.Fatal(got)
	}
	p.procs = nil
	pass(t, d, st, Plan{Procs: true})
	if got := sessions(t, st)[0]; got.EndedAt == nil || got.PID != 0 || got.Label != "dead" {
		t.Fatal(got)
	}
}
