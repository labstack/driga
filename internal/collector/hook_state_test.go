package collector

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
)

func TestSpoolOnlyHookState(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			d, st, p, a := setup(t)
			a.name, a.obs[0].Adapter = agent, agent
			a.obs[0].RegistryStatus = "idle"
			p.procs[0].Cmdline = agent
			now := d.now()
			d.Now = func() time.Time { return now }
			pass(t, d, st, Plan{Procs: true})
			appendHook := func(event, typ string) {
				t.Helper()
				now = now.Add(time.Second)
				// The JSONL envelope written by the hook command, including protocol metadata.
				raw, err := json.Marshal(map[string]any{"adapter": agent, "session_id": "one", "event": event, "notification_type": typ, "cwd": a.obs[0].Cwd, "occurred_at": now})
				if err != nil {
					t.Fatal(err)
				}
				var h model.HookEvent
				if err := json.Unmarshal(raw, &h); err != nil {
					t.Fatal(err)
				}
				if err := spool.Append(d.SpoolDir, h); err != nil {
					t.Fatal(err)
				}
				if err := drainOnly(context.Background(), d, st); err != nil {
					t.Fatal(err)
				}
			}
			check := func(want string, waiting bool) {
				t.Helper()
				s := sessions(t, st)[0]
				if s.Label != want || s.Mixer == nil || derive.SessionMixer(s).Verdict != want {
					t.Fatalf("stored label/mixer = %s/%+v; want %s", s.Label, s.Mixer, want)
				}
				rows, err := st.Inbox(time.Time{}, false)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, row := range rows {
					if row.Kind == "waiting" {
						found = true
					}
				}
				if found != waiting {
					t.Fatalf("waiting inbox = %v; want %v: %+v", found, waiting, rows)
				}
				// A subsequent full pass must produce the same verdict from stored events.
				pass(t, d, st, Plan{})
				if got := sessions(t, st)[0].Label; got != want {
					t.Fatalf("full pass = %s; want %s", got, want)
				}
			}
			appendHook("Stop", "")
			check("finished", false)
			waitEvent := "Notification"
			if agent == "codex" {
				waitEvent = "PermissionRequest"
			}
			appendHook(waitEvent, "permission_prompt")
			check("waiting", true)
			appendHook("Stop", "")
			check("finished", false)
			appendHook("SessionStart", "")
			check("idle", false)
			appendHook(waitEvent, "permission_prompt")
			check("waiting", true)
			appendHook("UserPromptSubmit", "")
			check("working", false)
			now = now.Add(d.Cfg.StalledAfter + time.Second)
			pass(t, d, st, Plan{})
			check("stalled", false)
			appendHook(waitEvent, "permission_prompt")
			check("waiting", true)
			appendHook("Notification", "idle_prompt")
			check("idle", false)
			appendHook(waitEvent, "permission_prompt")
			check("waiting", true)
			appendHook("SessionEnd", "")
			check("dead", false)
		})
	}
}
