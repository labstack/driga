package collector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/intake"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestIntakeWithoutFeedIgnoresUnrelatedSessions(t *testing.T) {
	d, st, _, _ := setup(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.ConfigureFeed(false) }); err != nil {
		t.Fatal(err)
	}
	b := intake.Batch{Facts: []intake.Fact{{SessionID: "not-discovered", Name: "api_request", TS: d.now()}}}
	measure := func() float64 {
		return testing.AllocsPerRun(3, func() {
			if unmatched, err := applyIntake(context.Background(), d, st, b); err != nil || unmatched != 1 {
				t.Fatalf("unmatched=%d, error=%v", unmatched, err)
			}
		})
	}
	// Initialize source/counter rows before measuring the steady state.
	measure()
	before := measure()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := range 100 {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i)}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after := measure()
	t.Logf("intake allocations without feed: before=%.0f; with 100 unrelated sessions=%.0f", before, after)
	if after > before+100 {
		t.Fatalf("unrelated sessions added %.0f intake allocations without feed consumers", after-before)
	}
}

func intakeCostFixture(t testing.TB) (Deps, *store.Store) {
	t.Helper()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	d := Deps{Cfg: config.Default(), Plat: &fakePlatform{}, Now: func() time.Time { return now }, State: NewState()}
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.ConfigureFeed(false); err != nil {
			return err
		}
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "target", PID: 101, ProcStart: now.Add(-time.Hour), RegistryStatus: "busy", LastActivityAt: now}})
		if err != nil {
			return err
		}
		// Fill the bounded event window so repeated known facts measure steady
		// state rather than a growing target history.
		for range 200 {
			if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "target", Kind: "otlp:api_request", TS: now}); err != nil {
				return err
			}
		}
		return tx.ReplaceProcs([]model.Proc{{PID: 101, StartTime: now.Add(-time.Hour), CPUPercent: 0.5, Argv: []string{"claude"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	return d, st
}

func addUnrelatedIntakeRows(t testing.TB, d Deps, st *store.Store) {
	t.Helper()
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		procs := []model.Proc{{PID: 101, StartTime: d.now().Add(-time.Hour), CPUPercent: 0.5, Argv: []string{"claude"}}}
		for i := range 2000 {
			procs = append(procs, model.Proc{PID: int32(1000 + i), StartTime: d.now().Add(-time.Hour), Cmdline: "unrelated process", Argv: []string{"unrelated", "--option", "value"}})
		}
		if err := tx.ReplaceProcs(procs); err != nil {
			return err
		}
		for i := range 1000 {
			key := fmt.Sprintf("unrelated-%d", i)
			id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: key}})
			if err != nil {
				return err
			}
			if err := tx.UpsertInboxItem(model.InboxItem{ID: key, SessionID: id, Kind: "finished", TS: d.now(), Summary: "unrelated completion"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func intakeCostBatch(name string, now time.Time) (intake.Batch, int) {
	switch name {
	case "metrics":
		return intake.Batch{MetricsRejected: 1}, 0
	case "unmatched":
		return intake.Batch{Facts: []intake.Fact{{SessionID: "absent", Name: "api_request", TS: now}}}, 1
	default:
		return intake.Batch{Facts: []intake.Fact{{SessionID: "target", Name: "api_request", TS: now}}}, 0
	}
}

func TestIntakeIgnoresUnrelatedProcessAndInboxRows(t *testing.T) {
	for _, name := range []string{"metrics", "unmatched", "known"} {
		t.Run(name, func(t *testing.T) {
			d, st := intakeCostFixture(t)
			batch, wantUnmatched := intakeCostBatch(name, d.now())
			measure := func() float64 {
				return testing.AllocsPerRun(3, func() {
					if unmatched, err := applyIntake(context.Background(), d, st, batch); err != nil || unmatched != wantUnmatched {
						t.Fatalf("unmatched=%d, error=%v", unmatched, err)
					}
				})
			}
			measure() // Warm counters, source status, and retained mixer JSON.
			before := measure()
			addUnrelatedIntakeRows(t, d, st)
			after := measure()
			t.Logf("allocations: baseline=%.0f, with 2000 unrelated processes and 1000 unseen completions=%.0f", before, after)
			if after > before+200 {
				t.Fatalf("unrelated process/inbox rows added %.0f allocations", after-before)
			}
		})
	}
}

func BenchmarkIntakeLargeUnrelatedState(b *testing.B) {
	for _, name := range []string{"metrics", "unmatched", "known"} {
		b.Run(name, func(b *testing.B) {
			d, st := intakeCostFixture(b)
			addUnrelatedIntakeRows(b, d, st)
			batch, wantUnmatched := intakeCostBatch(name, d.now())
			if _, err := applyIntake(context.Background(), d, st, batch); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if unmatched, err := applyIntake(context.Background(), d, st, batch); err != nil || unmatched != wantUnmatched {
					b.Fatalf("unmatched=%d, error=%v", unmatched, err)
				}
			}
		})
	}
}

type intakeSourcePlatform struct {
	*fakePlatform
	os string
}

func (p intakeSourcePlatform) ProcessOS() string { return p.os }

func TestIntakeUsesCurrentProcessIdentity(t *testing.T) {
	for _, os := range []string{"darwin", "linux", "windows"} {
		t.Run(os, func(t *testing.T) {
			d, st := intakeCostFixture(t)
			d.Plat = intakeSourcePlatform{fakePlatform: &fakePlatform{}, os: os}
			var err error
			d.State.procs, err = st.Procs()
			if err != nil {
				t.Fatal(err)
			}
			batch, _ := intakeCostBatch("known", d.now())
			for _, reused := range []bool{false, true} {
				start := d.now().Add(-time.Hour)
				if reused {
					start = d.now().Add(-time.Minute)
				}
				if err := st.Tx(context.Background(), func(tx *store.Tx) error {
					return tx.ReplaceProcs([]model.Proc{{PID: 101, StartTime: start, CPUPercent: 17}})
				}); err != nil {
					t.Fatal(err)
				}
				if _, err := applyIntake(context.Background(), d, st, batch); err != nil {
					t.Fatal(err)
				}
				s, err := st.SessionByKey("claude", "target")
				if err != nil || s.Mixer == nil || s.Mixer.Input == nil {
					t.Fatalf("retained label input: %+v, %v", s, err)
				}
				p := s.Mixer.Input.Proc
				if reused && p != nil {
					t.Fatalf("reused PID treated as owning process: %+v", p)
				}
				if !reused && (p == nil || p.CPUPercent != 17) {
					t.Fatalf("fresh process reading replaced by cached value: %+v", p)
				}
			}
		})
	}
}

func TestIntakeKeepsPerSessionUnseenCompletions(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs[0].RegistryStatus = "idle"
	a.obs[0].LastActivityAt = d.now().Add(-time.Hour)
	pass(t, d, st, Plan{Procs: true})
	first, err := st.SessionByKey("claude", "one")
	if err != nil {
		t.Fatal(err)
	}
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.ConfigureFeed(false); err != nil {
			return err
		}
		second := first
		second.ExternalID = "other"
		if _, err := tx.UpsertSession(second); err != nil {
			return err
		}
		for _, key := range []string{"one", "other"} {
			if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: key, Kind: "Stop", TS: d.now().Add(-time.Minute)}); err != nil {
				return err
			}
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "target-completion", SessionID: first.ID, Kind: "finished", TS: d.now().Add(-time.Minute)})
	})
	if err != nil {
		t.Fatal(err)
	}
	batch := intake.Batch{Facts: []intake.Fact{
		{SessionID: "one", Name: "mcp_server_connection", TS: d.now()},
		{SessionID: "other", Name: "mcp_server_connection", TS: d.now()},
	}}
	for _, acknowledged := range []bool{false, true} {
		if acknowledged {
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				_, err := tx.AckInbox("target-completion", d.now())
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := applyIntake(context.Background(), d, st, batch); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"one", "other"} {
			want := "idle"
			if key == "one" && !acknowledged {
				want = "finished"
			}
			s, err := st.SessionByKey("claude", key)
			if err != nil || s.Label != want {
				t.Fatalf("session=%s, acknowledged=%t: label=%s, want %s; error=%v", key, acknowledged, s.Label, want, err)
			}
		}
	}
}
