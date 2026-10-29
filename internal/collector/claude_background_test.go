package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func claudeBackgroundFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "claude", "2.1", "background", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func backgroundProcs(t *testing.T) []model.Proc {
	t.Helper()
	var procs []model.Proc
	if err := json.Unmarshal(claudeBackgroundFixture(t, "processes.json"), &procs); err != nil {
		t.Fatal(err)
	}
	return procs
}

func backgroundFile(t *testing.T, rel string, b []byte) string {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".claude", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClaudeBackgroundHelpersDoNotCreateSessions(t *testing.T) {
	for _, registry := range []bool{false, true} {
		t.Run(fmt.Sprint(registry), func(t *testing.T) {
			d, st, p, _ := setup(t)
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			p.procs = backgroundProcs(t)
			d.Adapters = []adapter.Adapter{claude.New()}
			if registry {
				backgroundFile(t, "sessions/1036.json", claudeBackgroundFixture(t, "registry.json"))
			}
			for range 2 {
				pass(t, d, st, Plan{Procs: true})
				if ss := sessions(t, st); len(ss) != 0 {
					t.Errorf("unclaimed daemon/spares created %d sessions", len(ss))
				}
				orphans, err := st.Orphans()
				if err != nil || len(orphans) != 0 {
					t.Fatalf("live daemon tree became leftovers: %+v, %v", orphans, err)
				}
			}
		})
	}
}

func TestClaudeBackgroundClaimedRegistryState(t *testing.T) {
	for _, tc := range []struct {
		status, want string
		transcript   bool
	}{
		{"idle", "idle", true},
		{"busy", "working", false},
		{"busy", "working", true},
	} {
		t.Run(fmt.Sprintf("%s/transcript=%t", tc.status, tc.transcript), func(t *testing.T) {
			d, st, p, _ := setup(t)
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			p.procs = backgroundProcs(t)
			d.Adapters = []adapter.Adapter{claude.New()}
			registry := strings.Replace(string(claudeBackgroundFixture(t, "registry.json")), `"status":"idle"`, `"status":"`+tc.status+`"`, 1)
			backgroundFile(t, "sessions/1036.json", []byte(registry))
			if tc.transcript {
				path := backgroundFile(t, "projects/project/background-conversation.jsonl", claudeBackgroundFixture(t, "transcript.jsonl"))
				// A registered transcript remains proof of a claim even if the
				// baseline skips its contents because it is old.
				old := d.Now().Add(-8 * 24 * time.Hour)
				if err := os.Chtimes(path, old, old); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				pass(t, d, st, Plan{Procs: true})
				ss := sessions(t, st)
				if len(ss) != 1 {
					t.Fatalf("want one claimed conversation, got %d sessions", len(ss))
				}
				s := ss[0]
				if s.ExternalID != "background-conversation" || s.PID != 1036 || s.RegistryStatus != tc.status || s.Label != tc.want || s.Heuristic || s.EndedAt != nil || !s.ConversationClaimed {
					t.Fatalf("claimed bg status did not reach state derivation: %+v", s)
				}
				if s.Mixer == nil || !s.Mixer.Rows[0].Supports {
					t.Fatalf("registry did not support the verdict: %+v", s.Mixer)
				}
			}
			if tc.status == "busy" {
				// Once observed busy, keep the conversation when it becomes idle
				// before a transcript is available, including collector restart.
				backgroundFile(t, "sessions/1036.json", claudeBackgroundFixture(t, "registry.json"))
				for range 2 {
					d.State = NewState()
					pass(t, d, st, Plan{Procs: true})
					if ss := sessions(t, st); len(ss) != 1 || ss[0].Label != "idle" || !ss[0].ConversationClaimed {
						t.Fatalf("claimed background conversation lost on idle: %+v", ss)
					}
				}
			}
		})
	}
}

func TestClaudeBackgroundHelpersRetireOldPlaceholders(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	p.procs = backgroundProcs(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for _, proc := range p.procs {
			_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprintf("pid:%d:%d", proc.PID, proc.StartTime.UnixNano()), PID: proc.PID, ProcStart: proc.StartTime, StartedAt: proc.StartTime}})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true})
	for _, s := range sessions(t, st) {
		if s.PID != 0 || s.EndedAt == nil {
			t.Errorf("old helper placeholder still live: %s", s.ExternalID)
		}
	}
}

func TestClaudeBackgroundIdleReplacementDetachesOldConversation(t *testing.T) {
	d, _, p, a := setup(t)
	st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p.procs = backgroundProcs(t)
	a.obs = []model.SessionObs{{Adapter: "claude", ExternalID: "conversation-A", Origin: "background", PID: p.procs[2].PID, ProcStart: p.procs[2].StartTime, RegistryStatus: "busy", LastActivityAt: d.Now()}}
	pass(t, d, st, Plan{Procs: true})
	old := sessions(t, st)[0]
	a.obs[0].ExternalID, a.obs[0].RegistryStatus = "spare-B", "idle"
	for range 2 {
		d.State = NewState()
		pass(t, d, st, Plan{Procs: true})
		ss := sessions(t, st)
		if len(ss) != 1 || ss[0].ID != old.ID {
			t.Fatalf("unclaimed replacement admitted: %+v", ss)
		}
		if s := ss[0]; s.PID != 0 || s.Hosted || s.TurnOpen || s.EndedAt == nil || s.RegistryStatus != "ended" {
			t.Errorf("old conversation still owns replacement PID: %+v", s)
		}
	}
}

func TestClaudeBackgroundUpgradeRetiresUnclaimedStoredSpare(t *testing.T) {
	for _, origin := range []string{"background", "background/temporary"} {
		t.Run(origin, func(t *testing.T) {
			d, st, p, a := setup(t)
			p.procs = backgroundProcs(t)
			claimed := model.SessionObs{Adapter: "claude", ExternalID: "claimed", Origin: origin, PID: p.procs[2].PID, ProcStart: p.procs[2].StartTime, RegistryStatus: "busy", LastActivityAt: d.Now()}
			spare := claimed
			spare.ExternalID, spare.RegistryStatus = "legacy-spare", "idle"
			spare.PID, spare.ProcStart = p.procs[3].PID, p.procs[3].StartTime
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				for _, o := range []model.SessionObs{spare, claimed} {
					if _, err := tx.UpsertSession(model.Session{SessionObs: o}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			claimed.RegistryStatus = "idle"
			a.obs = []model.SessionObs{spare, claimed}
			for range 3 {
				d.State = NewState()
				pass(t, d, st, Plan{Procs: true})
				live, err := st.Sessions(store.SessionFilter{})
				if err != nil || len(live) != 1 || live[0].ExternalID != claimed.ExternalID || live[0].PID != claimed.PID {
					t.Fatalf("upgrade must keep only the claimed busy-then-idle conversation: %+v, %v", live, err)
				}
				for _, s := range sessions(t, st) {
					if s.ExternalID == spare.ExternalID && (s.PID != 0 || s.Hosted || s.EndedAt == nil) {
						t.Fatalf("legacy spare retained process ownership: %+v", s)
					}
				}
			}
		})
	}
}

func TestClaudeBackgroundHelperRetirementRequiresProcessStart(t *testing.T) {
	start := time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                       string
		sessionStart, processStart time.Time
		retired                    bool
	}{
		{"matching", start, start, true},
		{"missing-session-start", time.Time{}, start, false},
		{"missing-process-start", start, time.Time{}, false},
		{"both-starts-missing", time.Time{}, time.Time{}, false},
		{"reused-pid", start, start.Add(time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ss := []model.Session{{ID: 1, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "pid:1036:old", PID: 1036, ProcStart: tc.sessionStart, TurnOpen: true}}}
			aliases := retireProcessAliases(ss, []model.Proc{{PID: 1036, Agent: "claude", Role: "helper", StartTime: tc.processStart}}, start.Add(2*time.Hour))
			if len(aliases) != 0 {
				t.Fatalf("helper retirement must not transfer ownership: %v", aliases)
			}
			if tc.retired {
				if ss[0].PID != 0 || ss[0].EndedAt == nil || ss[0].TurnOpen {
					t.Fatalf("matching helper was not retired: %+v", ss[0])
				}
			} else if ss[0].PID != 1036 || ss[0].EndedAt != nil || !ss[0].TurnOpen {
				t.Fatalf("retirement changed an unproven process identity: %+v", ss[0])
			}
		})
	}
}

func TestClaudeBackgroundHelperMemoryAndOrphans(t *testing.T) {
	procs := backgroundProcs(t)
	idx, err := derive.NewProcIndexForOS(procs, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	d, _, _, _ := setup(t)
	// Stale spawning ownership must not turn the managed daemon tree into
	// leftovers, even when the daemon itself has PPID 1.
	memory := make(map[string]int64)
	for _, p := range procs {
		memory[fmt.Sprintf("%d:%d", p.PID, p.StartTime.Unix())] = 99
	}
	if got := derive.FindOrphans(idx, nil, d.Sigs, d.Now(), 0, nil, derive.WithSpawnMemory(memory)); len(got) != 0 {
		t.Errorf("live daemon tree became orphans: %+v", got)
	}
	for pid, placement := range derive.PlaceProcesses(procs, nil, d.Sigs) {
		if placement.Attribution != nil || placement.Bucket != derive.BucketAgent {
			t.Errorf("unclaimed helper %d attributed to a session: %+v", pid, placement)
		}
	}
	// Helpers with no surviving daemon are still eligible for normal orphan
	// detection after reparenting; do not globally suppress all spare helpers.
	spare := procs[2]
	spare.PPID = 1
	idx, _ = derive.NewProcIndexForOS([]model.Proc{spare}, "darwin")
	if got := derive.FindOrphans(idx, nil, d.Sigs, d.Now(), 0, nil); len(got) != 1 || got[0].RootPID != spare.PID {
		t.Errorf("detached spare with dead daemon not recognized: %+v", got)
	}
	// Existing parent-chain attribution must retain helper memory when there
	// is a real owning conversation above the daemon.
	parent := model.Proc{PID: 50, PPID: 2, StartTime: procs[0].StartTime.Add(-time.Minute), Cmdline: "claude", RSS: 50}
	procs[0].PPID = parent.PID
	procs = append(procs, parent)
	ss := []model.Session{{ID: 7, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "real", PID: parent.PID, ProcStart: parent.StartTime}, Label: "idle"}}
	placements := derive.PlaceProcesses(procs, ss, d.Sigs)
	for _, p := range procs {
		got := placements[p.PID]
		if got.Attribution == nil || got.Attribution.SessionID != 7 || got.Memory != p.RSS {
			t.Errorf("helper memory lost for %d: %+v", p.PID, got)
		}
	}
}
