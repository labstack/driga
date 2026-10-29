package collector

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestArgvModeSessionLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		live bool
	}{
		{"TUI escaped sandbox", []string{"codex", "-c", `label="say \" sandbox words"`}, true},
		{"exec escaped app-server", []string{"codex", "-c", `label="say \" app-server words"`, "exec", "task"}, true},
		{"app-server array", []string{"codex", "-c", `sandbox_workspace_write.writable_roots=["/tmp/a", "/tmp/b"]`, "app-server"}, false},
		{"sandbox short equals", []string{"codex", `-c=sandbox_workspace_write.writable_roots=["/tmp/a", "/tmp/b"]`, "sandbox"}, false},
		{"Claude prompt", []string{"claude", "daemon run"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, st, p, a := setup(t)
			a.obs = nil
			proc := p.procs[0]
			proc.Cmdline, proc.Exe, proc.Argv = strings.Join(tc.argv, " "), tc.argv[0], tc.argv
			// Keep the spawning shell live on platforms without reparenting.
			parent := model.Proc{PID: proc.PPID, PPID: 1, StartTime: proc.StartTime.Add(-time.Minute), Exe: "zsh", Cmdline: "zsh", Argv: []string{"zsh"}}
			p.procs = []model.Proc{proc, parent}
			agent := tc.argv[0]
			id := fmt.Sprintf("pid:%d:%d", proc.PID, proc.StartTime.UnixNano())
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: agent, ExternalID: id, PID: proc.PID, ProcStart: proc.StartTime, StartedAt: proc.StartTime}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				d.State = NewState()
				pass(t, d, st, Plan{Procs: true})
				ss := sessions(t, st)
				if len(ss) != 1 {
					t.Fatalf("want one existing identity, got %+v", ss)
				}
				if live := ss[0].PID == proc.PID && ss[0].EndedAt == nil; live != tc.live {
					t.Errorf("session live=%t, want %t: PID=%d ended=%v", live, tc.live, ss[0].PID, ss[0].EndedAt)
				}
			}
		})
	}
}
