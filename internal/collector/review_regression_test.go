package collector

import (
	"context"
	"errors"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTranscriptPanelReplayBaselineAndRetention(t *testing.T) {
	d, st, _, a := setup(t)
	d.Cfg.Fleet.Serve = true
	now := d.now()
	old := now.UTC().Truncate(time.Hour).Add(-23*time.Hour - time.Nanosecond)
	a.obs[0].Events = []model.Event{{TS: old, Kind: "log:tool_call:old"}, {TS: now, Kind: "log:tool_call:initial"}}
	checkFeed := func(seq int64, want int) {
		t.Helper()
		n := 0
		for _, row := range feedRows(t, st, seq) {
			if row.Kind == "event" {
				n++
			}
		}
		if n != want {
			t.Fatalf("event feed=%d want%d", n, want)
		}
	}
	pass(t, d, st, Plan{})
	checkFeed(0, 0)
	rows, _ := st.PanelEvents("claude", "one", time.Time{})
	if len(rows) != 1 {
		t.Fatalf("old event resurrected %+v", rows)
	}
	_, _, seq, _ := st.FeedBounds()
	a.obs[0].Events = append(a.obs[0].Events, model.Event{TS: now.Add(time.Second), Kind: "log:tool_call:new"})
	pass(t, d, st, Plan{})
	checkFeed(seq, 1)
	_, _, seq, _ = st.FeedBounds()
	d.State = NewState()
	a.obs[0].Events = append(a.obs[0].Events, model.Event{TS: now.Add(2 * time.Second), Kind: "log:tool_call:restart"})
	pass(t, d, st, Plan{})
	checkFeed(seq, 0)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.Prune(now.Add(25 * time.Hour)) }); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return now.Add(25 * time.Hour) }
	pass(t, d, st, Plan{})
	checkFeed(seq, 0)
	rows, _ = st.PanelEvents("claude", "one", time.Time{})
	if len(rows) != 0 {
		t.Fatalf("pruned tool rows resurrected %+v", rows)
	}
}
func TestCPUFailurePreservesOtherMachineData(t *testing.T) {
	d, st, p, _ := setup(t)
	d.Plat = &cpuPlatform{fakePlatform: p, sample: model.MachineSample{TS: d.now(), MemTotal: 100, MemUsed: 50, DiskFree: 20, DiskTotal: 200, CPUError: "counter unavailable"}}
	report, err := Pass(context.Background(), d, st, Plan{Machine: true})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := st.LatestSample()
	if err != nil || sample.MemTotal != 100 || sample.DiskTotal != 200 || sample.CPUPercent != nil || report.SourceErrors["machine_cpu"] == "" || report.SourceErrors["machine"] != "" {
		t.Fatalf("sample=%+v report=%+v err=%v", sample, report, err)
	}
}

func TestOwnChatIdentityKeepsUserProjects(t *testing.T) {
	for _, prefix := range []bool{false, true} {
		name := "project named chat beside chat.key"
		if prefix {
			name = "user title and prompt with Chat preamble"
		}
		t.Run(name, func(t *testing.T) {
			d, st, _, a := setup(t)
			root := t.TempDir()
			project := filepath.Join(root, "chat")
			if err := os.MkdirAll(project, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "chat.key"), []byte("test marker"), 0600); err != nil {
				t.Fatal(err)
			}
			observation := a.obs[0]
			observation.ExternalID = "user-chat-project"
			observation.Cwd = project
			observation.PID = 0
			if prefix {
				observation.Cwd = filepath.Join(root, "ordinary-project")
				observation.Title = model.ChatPreamble + " user-owned work"
				observation.LastPrompt = model.ChatPreamble + " copied text"
			}
			a.obs = []model.SessionObs{observation}
			pass(t, d, st, Plan{})
			found := sessions(t, st)
			if len(found) != 1 || found[0].ExternalID != "user-chat-project" {
				t.Fatalf("user project incorrectly hidden: %+v", found)
			}
		})
	}
}

func TestRegisteredChatIdentityRemovesStoredSession(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs = nil
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "owned-cli-session", Cwd: filepath.Join(t.TempDir(), "chat")}, Label: "working"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveConversation(context.Background(), store.Conversation{ID: "our-chat", SessionID: "owned-cli-session", CreatedAt: d.now(), UpdatedAt: d.now()}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	if found := sessions(t, st); len(found) != 0 {
		t.Fatalf("registered Chat session remains stored: %+v", found)
	}
}

func TestTranscriptPanelBaselineWaitsForAdapterObservation(t *testing.T) {
	for _, gap := range []string{"error", "empty", "partial error"} {
		for _, omittedAdapter := range []bool{false, true} {
			name := gap
			if omittedAdapter {
				name += " with adapter name inferred"
			}
			t.Run(name, func(t *testing.T) {
				d, st, _, a := setup(t)
				d.Cfg.Fleet.Serve = true
				now := d.now()
				observation := a.obs[0]
				if omittedAdapter {
					observation.Adapter = ""
				}
				observation.Events = []model.Event{{TS: now.Add(-5 * time.Minute), Kind: "log:tool_call:initial"}}
				a.obs = []model.SessionObs{observation}
				pass(t, d, st, Plan{})
				_, _, seq, err := st.FeedBounds()
				if err != nil {
					t.Fatal(err)
				}
				assertEventFeed := func(want int) {
					t.Helper()
					n := 0
					for _, row := range feedRows(t, st, seq) {
						if row.Kind == "event" {
							n++
						}
					}
					if n != want {
						t.Fatalf("event feed=%d want%d after %s", n, want, gap)
					}
				}
				d.State = NewState()
				a.obs = nil
				if gap != "empty" {
					a.err = errors.New("adapter unavailable")
				}
				if gap == "partial error" {
					a.obs = []model.SessionObs{observation}
				}
				pass(t, d, st, Plan{})
				assertEventFeed(0)
				// Recovery replays a full transcript, including a retained event
				// that this collector instance has never observed before.
				a.err = nil
				observation.Events = append(observation.Events, model.Event{TS: now.Add(-3 * time.Minute), Kind: "log:tool_call:historical"})
				a.obs = []model.SessionObs{observation}
				pass(t, d, st, Plan{})
				assertEventFeed(0)
				events, err := st.PanelEvents("claude", "one", time.Time{})
				if err != nil || len(events) != 2 {
					t.Fatalf("recovery lost retained identities: %+v %v", events, err)
				}
				observation.Events = append(observation.Events, model.Event{TS: now, Kind: "log:tool_call:after-recovery"})
				a.obs = []model.SessionObs{observation}
				pass(t, d, st, Plan{})
				assertEventFeed(1)
			})
		}
	}
}
