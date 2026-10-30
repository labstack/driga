package collector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
)

func BenchmarkCollectorPass(b *testing.B) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	st, err := store.Open("", store.Memory)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	p := &fakePlatform{}
	a := &fakeAdapter{name: "claude"}
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 1600; i++ {
			obs := model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Cwd: "/synthetic", StartedAt: now.Add(-24 * time.Hour), LastActivityAt: now.Add(-time.Hour), ConversationClaimed: true}
			s := model.Session{SessionObs: obs, Label: "ended"}
			ended := now.Add(-time.Hour)
			s.EndedAt = &ended
			if i < 25 {
				s.EndedAt = nil
				s.Label = "working"
				obs.PID = int32(100 + i)
				obs.ProcStart = obs.StartedAt
				obs.RegistryStatus = "busy"
				s.SessionObs = obs
				a.obs = append(a.obs, obs)
				p.procs = append(p.procs, model.Proc{PID: obs.PID, StartTime: obs.ProcStart, Cmdline: "claude", Argv: []string{"claude"}, Cwd: obs.Cwd})
			}
			if _, err := tx.UpsertSession(s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	d := Deps{Cfg: config.Default(), Plat: p, Adapters: []adapter.Adapter{a}, Sigs: signatures.Builtin(), SpoolDir: b.TempDir(), State: NewState(), Now: func() time.Time { return now }}
	for range 2 {
		if _, err := Pass(context.Background(), d, st, Plan{Procs: true}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		now = now.Add(time.Second)
		if _, err := Pass(context.Background(), d, st, Plan{Procs: true}); err != nil {
			b.Fatal(err)
		}
	}
}
