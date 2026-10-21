package view

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestProcTreeToleratesStartSkew(t *testing.T) {
	start := time.Unix(1700000000, 0)
	idx := derive.NewProcIndex([]model.Proc{
		{PID: 10, PPID: 1, StartTime: start, RSS: 100},
		{PID: 11, PPID: 10, StartTime: start.Add(time.Second), RSS: 50},
	})
	s := model.Session{SessionObs: model.SessionObs{PID: 10, ProcStart: start.Add(-300 * time.Millisecond)}}
	if got := len(procTree(s, idx)); got != 2 {
		t.Fatalf("registry start 300ms off: got %d procs, want 2", got)
	}
	s.ProcStart = start.Add(-5 * time.Second)
	if got := len(procTree(s, idx)); got != 0 {
		t.Fatalf("pid reuse (5s off): got %d procs, want 0", got)
	}
}

func TestPeerProcessTreeUsesRecordedOS(t *testing.T) {
	for _, sourceOS := range []string{"linux", "windows"} {
		t.Run(sourceOS, func(t *testing.T) {
			start := testNow.Add(-time.Hour)
			var sid int64
			st := fixture(t, func(tx *store.Tx) error {
				peer := tx.ForMachine("peer")
				if err := peer.UpsertMachine(model.Machine{OS: sourceOS}); err != nil {
					return err
				}
				var err error
				sid, err = peer.UpsertSession(model.Session{SessionObs: model.SessionObs{
					Adapter: "test", ExternalID: "one", PID: 100, ProcStart: start,
					StartedAt: start, LastActivityAt: testNow,
				}, Label: "working"})
				if err != nil {
					return err
				}
				return peer.ReplaceProcs([]model.Proc{
					{PID: 100, StartTime: start, RSS: 100},
					// Windows creator PID now belongs to a younger process.
					{PID: 101, PPID: 100, StartTime: start.Add(-time.Minute), RSS: 50},
				})
			})
			v, err := Agent(st, sid, testNow, store.Scope{Machines: []string{"peer"}})
			want := 2
			if sourceOS == "windows" {
				want = 1
			}
			if err != nil || len(v.Procs) != want {
				t.Fatalf("peer procs: %+v %v, want %d", v.Procs, err, want)
			}
			agents, err := Agents(st, Filter{Scope: store.Scope{Machines: []string{"peer"}}}, testNow)
			if err != nil || len(agents.Rows) != 1 || agents.Rows[0].Mem != uint64(50+50*want) {
				t.Fatalf("peer memory: %+v %v", agents, err)
			}
		})
	}
}
