package collector

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
)

func TestCopilotHooksStateAndInbox(t *testing.T) {
	for _, drain := range []bool{false, true} {
		t.Run(map[bool]string{false: "pass", true: "spool-wake"}[drain], func(t *testing.T) {
			d, st, p, a := setup(t)
			t.Setenv("COPILOT_HOME", t.TempDir())
			a.obs = nil
			p.procs = []model.Proc{{PID: 42, StartTime: d.now().Add(-time.Hour), Cmdline: "copilot", Cwd: os.Getenv("HOME"), TTY: "/dev/pts/1"}}
			d.InboxRunner = inboxRunnerFunc(func(_ context.Context, n string, _ ...string) ([]byte, error) {
				if n == "tmux" {
					return []byte("/dev/pts/1 work:1.1\n"), nil
				}
				return nil, nil
			})
			at := d.now()
			d.Now = func() time.Time { return at }
			pass(t, d, st, Plan{Procs: true})
			ss := sessions(t, st)
			if len(ss) != 1 || ss[0].Adapter != "copilot" || ss[0].Label != "unknown" {
				t.Fatal(ss)
			}
			events := []struct {
				event, typ, state string
				wait              bool
			}{
				{"sessionStart", "", "idle", false}, {"userPromptSubmitted", "", "working", false},
				{"notification", "agent_idle", "working", false}, {"notification", "agent_completed", "working", false},
				{"notification", "shell_completed", "working", false}, {"notification", "shell_detached_completed", "working", false},
				{"notification", "permission_prompt", "waiting", true}, {"postToolUse", "", "working", false},
				{"notification", "permission_prompt", "waiting", true}, {"postToolUseFailure", "", "working", false},
				{"notification", "elicitation_dialog", "waiting", true}, {"userPromptSubmitted", "", "working", false},
				{"subagentStart", "", "working", false}, {"subagentStop", "", "working", false},
				{"notification", "permission_prompt", "waiting", true}, {"agentStop", "", "finished", false},
				{"notification", "elicitation_dialog", "waiting", true}, {"sessionEnd", "", "finished", false},
			}
			for _, tc := range events {
				at = at.Add(time.Second)
				h := model.HookEvent{TS: at, Adapter: "copilot", SessionID: "s", Event: tc.event, NotificationType: tc.typ, Cwd: p.procs[0].Cwd, PID: 42, ProcStart: p.procs[0].StartTime, Title: "First prompt"}
				if err := spool.Append(d.SpoolDir, h); err != nil {
					t.Fatal(err)
				}
				if drain {
					if err := drainOnly(context.Background(), d, st); err != nil {
						t.Fatal(err)
					}
				} else {
					pass(t, d, st, Plan{})
				}
				ss = sessions(t, st)
				var s model.Session
				for _, v := range ss {
					if v.ExternalID == "s" {
						s = v
					}
				}
				if s.ID == 0 || s.Label != tc.state || s.Hosted {
					t.Fatalf("%s/%s: %+v", tc.event, tc.typ, s)
				}
				rows, _ := st.Inbox(time.Time{}, false)
				waits := 0
				for _, r := range rows {
					if r.Kind == "waiting" {
						waits++
						if r.Jump == nil || r.Jump.Kind != "tmux" || r.SessionID != s.ID {
							t.Fatal(r)
						}
						want := "permission"
						if tc.typ == "elicitation_dialog" {
							want = "elicitation"
						}
						if r.Reason != want {
							t.Fatal(r)
						}
					}
				}
				if (waits == 1) != tc.wait {
					t.Fatalf("%s/%s waits=%+v", tc.event, tc.typ, rows)
				}
			}
			pass(t, d, st, Plan{})
			for _, s := range sessions(t, st) {
				if s.ExternalID == "s" && s.EndedAt == nil {
					t.Fatalf("end resurrected: %+v", s)
				}
			}
		})
	}
}

func TestCopilotWaitSessionIsolationAndPIDReuse(t *testing.T) {
	d, st, p, a := setup(t)
	t.Setenv("COPILOT_HOME", t.TempDir())
	a.obs = nil
	start := d.now().Add(-time.Hour)
	p.procs = []model.Proc{{PID: 42, StartTime: start, Cmdline: "copilot"}}
	at := d.now()
	d.Now = func() time.Time { return at }
	pass(t, d, st, Plan{Procs: true})
	for _, h := range []model.HookEvent{{Adapter: "copilot", SessionID: "one", Event: "notification", NotificationType: "permission_prompt", PID: 42, ProcStart: start}, {Adapter: "copilot", SessionID: "two", Event: "postToolUse"}} {
		at = at.Add(time.Second)
		h.TS = at
		if err := spool.Append(d.SpoolDir, h); err != nil {
			t.Fatal(err)
		}
		pass(t, d, st, Plan{})
	}
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].Reason != "permission" {
		t.Fatal(rows)
	}
	p.procs[0].StartTime = at.Add(time.Second)
	at = at.Add(2 * time.Second)
	pass(t, d, st, Plan{Procs: true})
	for _, s := range sessions(t, st) {
		if s.ExternalID == "one" && (s.PID != 0 || s.EndedAt == nil) {
			t.Fatalf("PID reused: %+v", s)
		}
	}
}

func TestCopilotWaitNotClearedByNotificationsOrSubagents(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	start := d.now().Add(-time.Hour)
	p.procs = []model.Proc{{PID: 42, StartTime: start, Cmdline: "copilot"}}
	at := d.now()
	d.Now = func() time.Time { return at }
	pass(t, d, st, Plan{Procs: true})
	for _, tc := range []struct{ event, typ string }{{"notification", "permission_prompt"}, {"notification", "agent_idle"}, {"notification", "shell_completed"}, {"subagentStart", ""}, {"subagentStop", ""}} {
		at = at.Add(time.Second)
		h := model.HookEvent{Adapter: "copilot", SessionID: "s", Event: tc.event, NotificationType: tc.typ, TS: at, PID: 42, ProcStart: start}
		if err := spool.Append(d.SpoolDir, h); err != nil {
			t.Fatal(err)
		}
		pass(t, d, st, Plan{})
		rows, _ := st.Inbox(time.Time{}, false)
		if len(rows) != 1 || rows[0].Reason != "permission" {
			t.Fatal(tc, rows)
		}
		for _, s := range sessions(t, st) {
			if s.ExternalID == "s" && s.Label != "waiting" {
				t.Fatal(tc, s)
			}
		}
	}
}

func TestCopilotLateWaitAndToolMetadata(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	start := d.now().Add(-time.Hour)
	p.procs = []model.Proc{{PID: 42, StartTime: start, Cmdline: "copilot"}}
	at := d.now()
	d.Now = func() time.Time { return at }
	pass(t, d, st, Plan{Procs: true})
	for _, h := range []model.HookEvent{
		{TS: at.Add(2 * time.Second), Event: "postToolUse", Tool: "shell"},
		{TS: at.Add(time.Second), Event: "notification", NotificationType: "permission_prompt"},
	} {
		h.Adapter, h.SessionID, h.PID, h.ProcStart = "copilot", "s", 42, start
		if err := spool.Append(d.SpoolDir, h); err != nil {
			t.Fatal(err)
		}
		at = at.Add(3 * time.Second)
		pass(t, d, st, Plan{})
	}
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 0 {
		t.Fatalf("delayed wait reopened after tool activity: %+v", rows)
	}
	events, err := st.Events("copilot", "s", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == "postToolUse" && (e.Metadata == nil || e.Metadata.Tool != "shell") {
			t.Fatalf("lost tool metadata: %+v", e)
		}
	}
}

func TestCopilotWithoutParentLivesByRecencyAndTitlesPolicy(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs = nil
	at := d.now()
	d.Now = func() time.Time { return at }
	for _, ev := range []string{"sessionStart", "userPromptSubmitted"} {
		if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: "copilot", SessionID: "s", TS: at, Event: ev, Cwd: "/repo", Title: "private prompt", Prompt: "private prompt"}); err != nil {
			t.Fatal(err)
		}
		pass(t, d, st, Plan{})
	}
	for _, s := range sessions(t, st) {
		if s.ExternalID == "s" && (s.EndedAt != nil || s.Label != "working" || s.Title != "repo" || s.LastPrompt != "") {
			t.Fatal(s)
		}
	}
	at = at.Add(25 * time.Hour)
	pass(t, d, st, Plan{})
	for _, s := range sessions(t, st) {
		if s.ExternalID == "s" && s.EndedAt == nil {
			t.Fatal("unbounded hook-only liveness", s)
		}
	}
	if n, err := st.Meta("hooks:copilot:received"); err != nil || n == "" || n == "0" {
		t.Fatal("missing hook event evidence", n, err)
	}
}

func TestCopilotUnlinkedWaitWithoutEarlierActivity(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs = nil
	if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: "copilot", SessionID: "s", TS: d.now(), Event: "notification", NotificationType: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	for _, s := range sessions(t, st) {
		if s.ExternalID == "s" && (s.EndedAt != nil || s.Label != "waiting" || !s.LastActivityAt.IsZero()) {
			t.Fatal(s)
		}
	}
}
