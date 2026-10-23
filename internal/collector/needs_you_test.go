package collector

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	attention "github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

type inboxRunnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f inboxRunnerFunc) Run(c context.Context, n string, a ...string) ([]byte, error) {
	return f(c, n, a...)
}

type fakeNotifier struct{ notes []attention.Notification }

func (f *fakeNotifier) Notify(_ context.Context, n attention.Notification) error {
	f.notes = append(f.notes, n)
	return nil
}

func TestNeedsYouAgentsMergeAndAlerts(t *testing.T) {
	d, st, _, _ := setup(t)
	ctx := context.Background()
	at := d.now()
	state := "blocked"
	reason := "permission"
	d.InboxRunner = inboxRunnerFunc(func(_ context.Context, n string, _ ...string) ([]byte, error) {
		if n == "tmux" {
			return nil, nil
		}
		return []byte(`[{"id":"background","state":"` + state + `","waitingFor":"` + reason + `","cwd":"/repo"}]`), nil
	})
	notifier := &fakeNotifier{}
	d.Notifier = notifier
	if _, err := Pass(ctx, d, st, Plan{Procs: true}); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].Source != "agents_cli" || rows[0].Reason != "permission" || rows[0].Jump == nil {
		t.Fatalf("CLI-only wait: %+v", rows)
	}
	id := rows[0].ID
	if len(notifier.notes) != 1 {
		t.Fatalf("collector did not alert: %+v", notifier.notes)
	}
	at = at.Add(time.Second)
	d.Now = func() time.Time { return at }
	h := model.HookEvent{TS: at, Adapter: "claude", SessionID: "background", Event: "Notification", NotificationType: "agent_needs_input"}
	if err := spool.Append(d.SpoolDir, h); err != nil {
		t.Fatal(err)
	}
	if _, err := Pass(ctx, d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].ID != id || rows[0].Source != "both" || rows[0].Reason != "question" {
		t.Fatal(rows)
	}
	state = "running"
	if _, err := Pass(ctx, d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.Inbox(time.Time{}, false)
	if len(rows) != 1 {
		t.Fatal("CLI cleared hook wait", rows)
	}
	if err := st.Tx(ctx, func(tx *store.Tx) error { _, err := tx.AckInbox(id, at); return err }); err != nil {
		t.Fatal(err)
	}
	state = "blocked"
	at = at.Add(time.Second)
	if _, err := Pass(ctx, d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	// A real unblocked -> blocked transition starts a second generation.
	rows, _ = st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].ID == id {
		t.Fatal(rows)
	}
}

func TestNeedsYouHookFirstAckSurvivesCLIPoll(t *testing.T) {
	d, st, _, _ := setup(t)
	at := d.now()
	d.Now = func() time.Time { return at }
	ctx := context.Background()
	pass(t, d, st, Plan{Procs: true})
	at = at.Add(time.Second)
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: at, Adapter: "claude", SessionID: "one", Event: "Notification", NotificationType: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	at = at.Add(time.Second)
	d.InboxRunner = inboxRunnerFunc(func(_ context.Context, n string, _ ...string) ([]byte, error) {
		if n == "tmux" {
			return nil, nil
		}
		return []byte(`[{"id":"one","state":"blocked","waitingFor":"permission"}]`), nil
	})
	pass(t, d, st, Plan{})
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	if err := st.Tx(ctx, func(tx *store.Tx) error { _, e := tx.AckInbox(rows[0].ID, at); return e }); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	pass(t, d, st, Plan{})
	rows, _ = st.Inbox(time.Time{}, false)
	if len(rows) != 0 {
		t.Fatalf("CLI reopened acknowledged hook generation: %+v", rows)
	}
}

func TestNeedsYouRemoteAlertsRequirePairing(t *testing.T) {
	d, st, _, _ := setup(t)
	n := &fakeNotifier{}
	d.Notifier = n
	at := d.now()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ForMachine("unpaired").ApplyInbox(model.InboxItem{ID: "remote-wait", SessionID: 2, Kind: "waiting", TS: at, WaitingSinceAt: at, Reason: "permission"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 0 {
		t.Fatal("alerted for unpaired machine", n.notes)
	}
	if err := fleet.SavePeers(filepath.Join(config.DataDir(), "fleet"), []fleet.Peer{{MachineID: "unpaired", DeviceID: "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 1 {
		t.Fatal("paired viewer did not alert", n.notes)
	}
}

func TestNeedsYouGrowthAfterHookInSamePass(t *testing.T) {
	d, st, _, a := setup(t)
	ctx := context.Background()
	at := d.now()
	d.Now = func() time.Time { return at }
	path := filepath.Join(t.TempDir(), "transcript")
	a.obs[0].SourcePath = path
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true})
	at = at.Add(time.Second)
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: at, Adapter: "claude", SessionID: "one", Event: "Notification", NotificationType: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	if err := os.WriteFile(path, []byte("xx"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Pass(ctx, d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Inbox(time.Time{}, true)
	if len(rows) != 1 || rows[0].EndedAt == nil || rows[0].AckedAt != nil {
		t.Fatalf("missed same-pass growth: %+v", rows)
	}
}

func TestNeedsYouOptionalAgentsFailureStaysQuiet(t *testing.T) {
	d, st, _, _ := setup(t)
	d.InboxRunner = inboxRunnerFunc(func(context.Context, string, ...string) ([]byte, error) { return nil, exec.ErrNotFound })
	r, err := Pass(context.Background(), d, st, Plan{Procs: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.SourceErrors["agents_cli"] != "" {
		t.Fatal("optional missing CLI raised attention", r.SourceErrors)
	}
}

func TestNeedsYouRemoteQuietWhenPresent(t *testing.T) {
	d, st, _, _ := setup(t)
	at := d.now()
	d.Cfg.Inbox.AlertAfter = time.Minute
	n := &fakeNotifier{}
	d.Notifier = n
	if err := fleet.SavePeers(filepath.Join(config.DataDir(), "fleet"), []fleet.Peer{{MachineID: "paired", DeviceID: "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		r := tx.ForMachine("paired")
		id, err := r.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote", LastActivityAt: at.Add(-time.Second)}})
		if err != nil {
			return err
		}
		return r.ApplyInbox(model.InboxItem{ID: "remote-wait", SessionID: id, Kind: "waiting", TS: at, WaitingSinceAt: at, Reason: "permission"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := alertInbox(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if len(n.notes) != 0 {
		t.Fatal("ignored paired session activity", n.notes)
	}
}

func TestNeedsYouPassLifecycle(t *testing.T) {
	d, st, _, a := setup(t)
	ctx := context.Background()
	if _, err := Pass(ctx, d, st, Plan{Procs: true}); err != nil {
		t.Fatal(err)
	}
	at := d.now().Add(time.Second)
	d.Now = func() time.Time { return at }
	write := func(typ, event string, when time.Time) {
		t.Helper()
		var h model.HookEvent
		b, _ := json.Marshal(map[string]any{"adapter": "claude", "session_id": "one", "event": event, "notification_type": typ, "occurred_at": when, "detail": "private command"})
		json.Unmarshal(b, &h)
		if err := spool.Append(d.SpoolDir, h); err != nil {
			t.Fatal(err)
		}
	}
	write("permission_prompt", "Notification", at)
	if _, err := Pass(ctx, d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	id := rows[0].ID
	at = at.Add(time.Second)
	write("agent_needs_input", "Notification", at)
	if _, err := Pass(ctx, d, st, Plan{}); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatal(rows)
	}
	at = at.Add(time.Second)
	a.obs[0].LastActivityAt = at
	write("", "Stop", at)
	if err := drainOnly(ctx, d, st); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.Inbox(time.Time{}, false)
	for _, i := range rows {
		if i.Kind == "waiting" {
			t.Fatalf("Stop left wait active: %+v", i)
		}
	}
	hist, _ := st.Inbox(time.Time{}, true)
	for _, i := range hist {
		if i.ID == id {
			b, _ := json.Marshal(i)
			var v map[string]any
			json.Unmarshal(b, &v)
			if v["ended_at"] == nil || i.AckedAt != nil || v["detail"] != nil {
				t.Fatalf("end/policy: %s", b)
			}
		}
	}
}

// A registry may be written seconds after exec, so the recorded start can be
// later than the kernel's. SameStart takes (kernel, recorded); swapped, such a
// session lost its tmux jump.
func TestNeedsYouJumpWhenRegistryWrittenAfterExec(t *testing.T) {
	d, st, p, a := setup(t)
	p.procs[0].TTY = "/dev/ttys009"
	a.obs[0].ProcStart = p.procs[0].StartTime.Add(5 * time.Second)
	d.InboxRunner = inboxRunnerFunc(func(_ context.Context, n string, _ ...string) ([]byte, error) {
		if n == "tmux" {
			return []byte("/dev/ttys009 work:1.1 /tmp/tmux-test/default\n"), nil
		}
		return nil, exec.ErrNotFound
	})
	at := d.now()
	d.Now = func() time.Time { return at }
	pass(t, d, st, Plan{Procs: true})
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: at.Add(time.Second), Adapter: "claude", SessionID: "one", Event: "Notification", NotificationType: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(2 * time.Second)
	pass(t, d, st, Plan{Procs: true})
	rows, _ := st.Inbox(time.Time{}, false)
	if len(rows) != 1 || rows[0].Jump == nil || rows[0].Jump.Kind != "tmux" || rows[0].Jump.Target != "work:1.1" {
		t.Fatalf("jump = %+v %+v", rows, rows[0].Jump)
	}
}
