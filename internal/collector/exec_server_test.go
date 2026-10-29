package collector

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestExecServerSessionLifecycle(t *testing.T) {
	for _, mode := range []string{"exec-server", "exec"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy=%t", mode, legacy), func(t *testing.T) {
				d, st, p, _ := setup(t)
				d.Adapters = []adapter.Adapter{codex.New()}
				p.procs = helperFixture(t, "codex/helpers/exec-server.json")
				proc := &p.procs[1]
				proc.Argv[3] = mode
				proc.Cmdline = strings.Join(proc.Argv, " ")
				id := fmt.Sprintf("pid:%d:%d", proc.PID, proc.StartTime.UnixNano())
				if legacy {
					if err := st.Tx(context.Background(), func(tx *store.Tx) error {
						_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: id, PID: proc.PID, ProcStart: proc.StartTime, StartedAt: proc.StartTime, Origin: "interactive"}, Label: "unknown"})
						return err
					}); err != nil {
						t.Fatal(err)
					}
				}
				for range 2 {
					d.State = NewState()
					pass(t, d, st, Plan{Procs: true})
					ss := sessions(t, st)
					if mode == "exec" {
						if len(ss) != 1 || ss[0].ExternalID != id || ss[0].PID != proc.PID || ss[0].EndedAt != nil || ss[0].Origin != "headless" {
							t.Fatalf("exec lost headless session: %+v", ss)
						}
					} else if !legacy {
						if len(ss) != 0 {
							t.Fatalf("exec-server created a session/placeholder: %+v", ss)
						}
					} else if len(ss) != 1 || ss[0].ExternalID != id || ss[0].PID != 0 || ss[0].EndedAt == nil {
						t.Fatalf("legacy exec-server placeholder still live: %+v", ss)
					}
				}
			})
		}
	}
}

func TestExecServerRetirementGuards(t *testing.T) {
	proc := helperFixture(t, "codex/helpers/exec-server.json")[1]
	for _, tc := range []struct {
		name   string
		change func(*model.Session, *model.Proc)
		retire bool
	}{
		{"matching", func(*model.Session, *model.Proc) {}, true},
		{"missing session start", func(s *model.Session, _ *model.Proc) { s.ProcStart = time.Time{} }, false},
		{"missing process start", func(_ *model.Session, p *model.Proc) { p.StartTime = time.Time{} }, false},
		{"reused PID", func(_ *model.Session, p *model.Proc) { p.StartTime = p.StartTime.Add(time.Hour) }, false},
		{"different adapter", func(s *model.Session, _ *model.Proc) { s.Adapter = "claude" }, false},
		{"real conversation", func(s *model.Session, _ *model.Proc) { s.ExternalID = "real-conversation" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, _, _ := setup(t)
			p := proc
			sig, _ := d.Sigs.Match(p.Cmdline, p.Exe, p.Argv)
			p.Agent, p.Role = sig.Agent, string(sig.Role)
			s := model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "pid:1101:legacy", PID: p.PID, ProcStart: p.StartTime, TurnOpen: true}}
			tc.change(&s, &p)
			ss := []model.Session{s}
			aliases := retireProcessAliases(ss, []model.Proc{p}, d.Now())
			if len(aliases) != 0 {
				t.Fatalf("helper unexpectedly replaced a conversation: %v", aliases)
			}
			if tc.retire {
				if ss[0].PID != 0 || ss[0].EndedAt == nil || ss[0].TurnOpen {
					t.Errorf("matching placeholder not retired: %+v", ss[0])
				}
			} else if ss[0].PID != s.PID || ss[0].EndedAt != nil || !ss[0].TurnOpen {
				t.Errorf("identity guard changed unrelated session: %+v", ss[0])
			}
		})
	}
}
