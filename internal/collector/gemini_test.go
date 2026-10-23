package collector

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestGeminiFoldReplacesStoredState(t *testing.T) {
	d, st, p, a := setup(t)
	a.name = "gemini"
	p.procs[0].Cmdline = "gemini"
	p.procs[0].Argv = []string{"gemini"}
	a.obs = []model.SessionObs{{Adapter: "gemini", ExternalID: "synthetic", Cwd: p.procs[0].Cwd, StartedAt: p.procs[0].StartTime, Title: "Before rewind", LastActivityAt: d.Now(), LastPromptAt: d.Now(), TurnOpen: true}}
	pass(t, d, st, Plan{Procs: true})
	s := sessions(t, st)[0]
	if s.PID != 101 || s.Label != "working" {
		t.Fatal(s)
	}
	previous := d.Now().Add(-time.Minute)
	a.obs[0].LastPromptAt = previous
	a.obs[0].LastActivityAt = previous
	a.obs[0].TurnOpen = false
	a.obs[0].Title = ""
	pass(t, d, st, Plan{})
	s = sessions(t, st)[0]
	if !s.LastPromptAt.Equal(previous) || s.TurnOpen || s.Title != "" {
		t.Fatalf("stale pre-rewind state survived: %+v", s)
	}
}
