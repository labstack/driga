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
	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
)

func helperFixture(t *testing.T, name string) []model.Proc {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	var procs []model.Proc
	if err := json.Unmarshal(b, &procs); err != nil {
		t.Fatal(err)
	}
	return procs
}

func TestAgentHelpersNeverCreateSessionsAndRetirePlaceholders(t *testing.T) {
	for _, fixture := range []string{"claude/2.1/chrome-native-host/processes.json", "codex/helpers/sandboxes.json", "codex/helpers/app-server.json"} {
		t.Run(fixture, func(t *testing.T) {
			d, st, p, a := setup(t)
			a.obs = nil
			p.procs = helperFixture(t, fixture)
			for range 2 {
				pass(t, d, st, Plan{Procs: true})
				if ss := sessions(t, st); len(ss) != 0 {
					t.Errorf("infrastructure created %d sessions", len(ss))
				}
			}
			// Seed exactly the provisional identities an older collector made.
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				for _, proc := range p.procs {
					sig, ok := d.Sigs.Match(proc.Cmdline, proc.Exe, proc.Argv)
					if !ok || sig.Agent == "" {
						continue
					}
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: sig.Agent, ExternalID: fmt.Sprintf("pid:%d:%d", proc.PID, proc.StartTime.UnixNano()), PID: proc.PID, ProcStart: proc.StartTime, StartedAt: proc.StartTime}})
					if err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				d.State = NewState()
				pass(t, d, st, Plan{Procs: true})
				for _, s := range sessions(t, st) {
					if s.PID != 0 || s.EndedAt == nil {
						t.Errorf("legacy placeholder still owns infrastructure: %s", s.ExternalID)
					}
				}
			}
			// Helpers still contribute their memory to a proven ancestor.
			parent := model.Proc{PID: 50, PPID: 2, StartTime: p.procs[0].StartTime.Add(-time.Minute), Cmdline: "claude", RSS: 50}
			present := make(map[int32]bool)
			for _, proc := range p.procs {
				present[proc.PID] = true
			}
			for i := range p.procs {
				if p.procs[i].PPID == 1 || !present[p.procs[i].PPID] {
					p.procs[i].PPID = parent.PID
				}
			}
			p.procs = append(p.procs, parent)
			a.obs = []model.SessionObs{{Adapter: "claude", ExternalID: "real-ancestor", PID: parent.PID, ProcStart: parent.StartTime, StartedAt: parent.StartTime, LastActivityAt: d.Now(), RegistryStatus: "busy"}}
			pass(t, d, st, Plan{Procs: true})
			var owner int64
			for _, s := range sessions(t, st) {
				if s.ExternalID == "real-ancestor" {
					owner = s.ID
				}
			}
			stored, err := st.Procs()
			if err != nil {
				t.Fatal(err)
			}
			for _, proc := range stored {
				if owner == 0 || proc.SessionID != owner {
					t.Errorf("PID %d lost ancestor ownership: got %d, want %d", proc.PID, proc.SessionID, owner)
				}
			}
			placements := derive.PlaceProcesses(p.procs, sessions(t, st), d.Sigs)
			for _, proc := range p.procs {
				placement := placements[proc.PID]
				if placement.Attribution == nil || placement.Attribution.SessionID != owner || placement.Memory != proc.RSS {
					t.Errorf("PID %d lost ancestor memory: %+v", proc.PID, placement)
				}
			}
		})
	}
}

func TestChromeAndDaemonTreesNeverBecomeOrphans(t *testing.T) {
	for _, fixture := range []string{"claude/2.1/chrome-native-host/processes.json", "codex/helpers/app-server.json"} {
		t.Run(fixture, func(t *testing.T) {
			d, st, p, a := setup(t)
			a.obs = nil
			p.procs = helperFixture(t, fixture)
			if strings.HasPrefix(fixture, "claude/") {
				p.procs = p.procs[:1] // Chrome has exited; its native host remains.
			} else {
				const exe = "/Users/fixture/.codex/packages/app-server-daemon/bin/codex"
				p.procs[0].Exe, p.procs[0].Argv = exe, []string{exe, "app-server"}
				p.procs[0].Cmdline = strings.Join(p.procs[0].Argv, " ")
			}
			// Chrome may exit or an old placeholder's spawning owner may die.
			p.procs[0].PPID = 1
			memory := make(map[string]int64)
			for _, proc := range p.procs {
				memory[fmt.Sprintf("%d:%d", proc.PID, proc.StartTime.Unix())] = 99
			}
			idx, err := derive.NewProcIndexForOS(p.procs, "darwin")
			if err != nil {
				t.Fatal(err)
			}
			if got := derive.FindOrphans(idx, nil, d.Sigs, d.Now(), 0, nil, derive.WithSpawnMemory(memory)); len(got) != 0 {
				t.Errorf("managed tree became orphan candidates: %+v", got)
			}
			for range 2 {
				pass(t, d, st, Plan{Procs: true, Sockets: true})
				if got, err := st.Orphans(); err != nil || len(got) != 0 {
					t.Errorf("managed tree persisted as leftovers: %+v, %v", got, err)
				}
			}
		})
	}
}

func TestAppServerHostPlaceholderLifecycle(t *testing.T) {
	for _, argv := range [][]string{
		{"codex", "app-server", "--listen", "stdio://"},
		helperFixture(t, "codex/helpers/app-server.json")[0].Argv,
		{"codex", "-c", `sandbox_workspace_write.writable_roots=["/tmp/a", "/tmp/b"]`, "app-server"},
	} {
		t.Run(strings.Join(argv, "|"), func(t *testing.T) {
			d, st, p, _ := setup(t)
			d.Adapters = []adapter.Adapter{codex.New()}
			host := p.procs[0]
			host.Cmdline, host.Exe, host.Argv = strings.Join(argv, " "), argv[0], argv
			const desktopExe = "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT"
			parent := model.Proc{PID: host.PPID, PPID: 1, StartTime: host.StartTime.Add(-time.Minute), Exe: desktopExe, Cmdline: desktopExe, Argv: []string{desktopExe}}
			p.procs = []model.Proc{parent, host}
			sig, ok := d.Sigs.Match(host.Cmdline, host.Exe, host.Argv)
			if !ok || sig.Role != signatures.RoleHost {
				t.Fatalf("app-server must remain a host: %+v, %t", sig, ok)
			}
			for range 2 {
				d.State = NewState()
				pass(t, d, st, Plan{Procs: true})
				if ss := sessions(t, st); len(ss) != 0 {
					t.Fatalf("host with no conversation created a placeholder: %+v", ss)
				}
			}
			// A collector upgrade must also remove an old unknown process row.
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: fmt.Sprintf("pid:%d:%d", host.PID, host.StartTime.UnixNano()), PID: host.PID, ProcStart: host.StartTime, StartedAt: host.StartTime, Origin: "interactive"}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				d.State = NewState()
				pass(t, d, st, Plan{Procs: true})
				ss := sessions(t, st)
				if len(ss) != 1 || ss[0].PID != 0 || ss[0].EndedAt == nil {
					t.Fatalf("legacy host placeholder still live: %+v", ss)
				}
			}
		})
	}
}

func TestCodexHelperRetirementRequiresProcessIdentity(t *testing.T) {
	start := time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC)
	for _, role := range []string{"helper", "service", "host"} {
		for _, tc := range []struct {
			name                       string
			sessionStart, processStart time.Time
			retire                     bool
		}{
			{"matching", start, start, true},
			{"missing-session-start", time.Time{}, start, false},
			{"missing-process-start", start, time.Time{}, false},
			{"both-missing", time.Time{}, time.Time{}, false},
			{"reused-pid", start, start.Add(time.Hour), false},
		} {
			t.Run(role+"/"+tc.name, func(t *testing.T) {
				ss := []model.Session{{ID: 7, SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "pid:1035:old", PID: 1035, ProcStart: tc.sessionStart, TurnOpen: true}}}
				got := retireProcessAliases(ss, []model.Proc{{PID: 1035, Agent: "codex", Role: role, StartTime: tc.processStart}}, start.Add(2*time.Hour))
				if len(got) != 0 || (ss[0].EndedAt != nil) != tc.retire || tc.retire && (ss[0].PID != 0 || ss[0].TurnOpen) || !tc.retire && (ss[0].PID != 1035 || !ss[0].TurnOpen) {
					t.Errorf("retirement changed the wrong process: %+v, aliases=%v", ss[0], got)
				}
			})
		}
	}
}

func TestAppServerKeepsRealConversations(t *testing.T) {
	for _, cmd := range []string{"codex app-server --listen stdio://", "codex -c features.code_mode_host=true app-server", "/Users/fixture/.codex/packages/app-server-daemon/bin/codex app-server"} {
		t.Run(cmd, func(t *testing.T) {
			d, st, p, _ := setup(t)
			p.procs = helperFixture(t, "codex/helpers/app-server.json")[:1]
			p.procs[0].Cmdline, p.procs[0].Argv = cmd, strings.Fields(cmd)
			// ChatGPT's shared host still has its live desktop parent.
			p.procs[0].PPID = 2
			const desktopExe = "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT"
			p.procs = append(p.procs, model.Proc{PID: 2, PPID: 1, StartTime: p.procs[0].StartTime.Add(-time.Minute), Exe: desktopExe, Cmdline: desktopExe, Argv: []string{desktopExe}})
			d.Adapters = []adapter.Adapter{codex.New()}
			windowsIdx, err := derive.NewProcIndexForOS(p.procs, "windows")
			if err != nil {
				t.Fatal(err)
			}
			if got := derive.FindOrphans(windowsIdx, nil, d.Sigs, d.Now(), 0, nil); len(got) != 0 {
				t.Errorf("bare app-server became leftovers under Windows semantics: %+v", got)
			}
			pass(t, d, st, Plan{Procs: true})
			if ss := sessions(t, st); len(ss) != 0 {
				t.Errorf("bare app-server created placeholders: %+v", ss)
			}
			if got, err := st.Orphans(); err != nil || len(got) != 0 {
				t.Errorf("bare app-server became leftovers: %+v, %v", got, err)
			}
			b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "codex", "helpers", "rollout-app-server.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "2026", "10", "18", "rollout-app-conversation.jsonl")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			pass(t, d, st, Plan{Procs: true})
			ss := sessions(t, st)
			if len(ss) != 1 || ss[0].ExternalID != "app-conversation" || !ss[0].Hosted || ss[0].EndedAt != nil || ss[0].PID != 0 || ss[0].SourcePath != path {
				t.Fatalf("real hosted rollout hidden: %+v", ss)
			}
			// Shared hosts and daemons can own real child conversations. Preserve the
			// existing ancestry/cwd/time evidence used for its family grouping.
			host := p.procs[0]
			host.PPID, host.Cwd = 50, ss[0].Cwd
			sig, _ := d.Sigs.Match(host.Cmdline, host.Exe, host.Argv)
			host.Role = string(sig.Role)
			parentProc := model.Proc{PID: 50, PPID: 2, StartTime: host.StartTime.Add(-time.Minute), Cmdline: "claude", Cwd: host.Cwd}
			parent := model.Session{ID: ss[0].ID + 100, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "parent", PID: 50, ProcStart: parentProc.StartTime, StartedAt: parentProc.StartTime, Cwd: host.Cwd}}
			idx, err := derive.NewProcIndexForOS([]model.Proc{parentProc, host}, "darwin")
			if err != nil {
				t.Fatal(err)
			}
			if links := derive.LinkParents(append(ss, parent), idx); links[ss[0].ID] != parent.ID {
				t.Errorf("hosted conversation lost family evidence: %v", links)
			}
			// A real conversation may already have a persisted process binding.
			// Infrastructure retirement must only remove provisional identities.
			ss[0].PID, ss[0].ProcStart = p.procs[0].PID, p.procs[0].StartTime
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				_, err := tx.UpsertSession(ss[0])
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				d.State = NewState()
				pass(t, d, st, Plan{Procs: true})
				ss := sessions(t, st)
				if len(ss) != 1 || ss[0].ExternalID != "app-conversation" || ss[0].PID != p.procs[0].PID || ss[0].EndedAt != nil {
					t.Errorf("process-bound conversation hidden: %+v", ss)
				}
			}
		})
	}
}
