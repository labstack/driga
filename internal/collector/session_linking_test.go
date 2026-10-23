package collector

import (
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestNewCodexSessionLinkedSamePass(t *testing.T) {
	d, st, p, a := setup(t)
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	a.name = "codex"
	a.obs = []model.SessionObs{{Adapter: "codex", ExternalID: "new", Cwd: t.TempDir(), StartedAt: d.Now()}}
	p.procs = append(p.procs, model.Proc{PID: 202, PPID: 2, StartTime: d.Now(), Cmdline: "codex", Argv: []string{"codex"}})
	p.cwds = map[int32]string{202: a.obs[0].Cwd}
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	for _, s := range sessions(t, st) {
		if s.ExternalID == "new" {
			if s.PID != 202 || s.EndedAt != nil {
				t.Fatalf("not linked in discovery pass: %+v", s)
			}
			return
		}
	}
	t.Fatal("new session missing")
}
