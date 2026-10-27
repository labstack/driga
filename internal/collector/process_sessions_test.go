package collector

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
)

func TestProcessReplacementTransfersHelperOwnership(t *testing.T) {
	for _, source := range []string{"registry", "rollout", "claude-hook", "codex-hook", "registry-without-scan"} {
		t.Run(source, func(t *testing.T) {
			agent := "claude"
			if source == "rollout" || source == "codex-hook" {
				agent = "codex"
			}
			d, st, p, a := setup(t)
			obs := a.obs[0]
			a.name, a.obs = agent, nil
			p.procs[0].Cmdline, p.procs[0].Argv = agent, []string{agent}
			helper := model.Proc{PID: 200, PPID: 101, StartTime: p.procs[0].StartTime.Add(time.Second), Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}}
			p.procs = append(p.procs, helper)
			probe := &socketPlatform{fakePlatform: p, peers: map[int32]bool{200: false}}
			d.Plat = probe
			now := d.now()
			d.Now = func() time.Time { return now }
			pass(t, d, st, Plan{Procs: true, Sockets: true})
			key := fmt.Sprintf("%d:%d", helper.PID, helper.StartTime.Unix())
			provisional := sessions(t, st)[0].ID
			if d.State.spawn[key] != provisional {
				t.Fatal("provisional session did not own helper")
			}
			obs.Adapter = agent
			if agent == "codex" {
				obs.PID, obs.ProcStart, obs.RegistryStatus = 0, time.Time{}, ""
			}
			if source == "claude-hook" || source == "codex-hook" {
				if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: agent, SessionID: obs.ExternalID, Event: "SessionStart", TS: now, PID: p.procs[0].PID, ProcStart: p.procs[0].StartTime, Cwd: obs.Cwd}); err != nil {
					t.Fatal(err)
				}
			} else {
				a.obs = []model.SessionObs{obs}
			}
			if source == "registry-without-scan" {
				pass(t, d, st, Plan{})
			}
			for i := 0; i < 4; i++ {
				now = now.Add(time.Second)
				probe.requested = nil
				pass(t, d, st, Plan{Procs: true, Sockets: true})
				if len(probe.requested) != 0 {
					t.Fatalf("pass %d: live helper became socket candidate: %v", i, probe.requested)
				}
				orphans, err := st.Orphans()
				if err != nil || len(orphans) != 0 {
					t.Fatalf("pass %d: live helper became orphan: %+v, %v", i, orphans, err)
				}
				var owner int64
				for _, s := range sessions(t, st) {
					if s.ExternalID == obs.ExternalID {
						owner = s.ID
					} else if s.ID == provisional && (s.PID != 0 || s.EndedAt == nil) {
						t.Fatalf("provisional row still live: %+v", s)
					}
				}
				procs, err := st.Procs()
				if err != nil {
					t.Fatal(err)
				}
				for _, proc := range procs {
					if proc.PID == helper.PID && (owner == 0 || proc.SessionID != owner || d.State.spawn[key] != owner) {
						t.Fatalf("pass %d: helper owner=%d, memory=%d, want %d", i, proc.SessionID, d.State.spawn[key], owner)
					}
				}
			}
		})
	}
}

func TestHookOnlySessionReplacesProcessPlaceholder(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		for _, parent := range []string{"agent", "shell", "reused", "missing-start"} {
			t.Run(agent+"/"+parent, func(t *testing.T) {
				d, st, p, a := setup(t)
				a.name, a.obs = agent, nil
				p.procs[0].Cmdline, p.procs[0].Argv = agent, []string{agent}
				now := d.now()
				d.Now = func() time.Time { return now }
				pass(t, d, st, Plan{Procs: true})
				identity := p.procs[0]
				if parent == "shell" {
					identity = model.Proc{PID: 201, PPID: identity.PID, StartTime: now.Add(-time.Minute), Cmdline: "sh", Argv: []string{"sh"}}
					p.procs = append(p.procs, identity)
				}
				if parent == "reused" {
					identity.StartTime = identity.StartTime.Add(-time.Hour)
				}
				if parent == "missing-start" {
					identity.StartTime = time.Time{}
				}
				for i := 0; i < 3; i++ {
					now = now.Add(time.Second)
					if i < 2 {
						if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: agent, SessionID: "hook-only", Event: "SessionStart", TS: now, PID: identity.PID, ProcStart: identity.StartTime, Cwd: identity.Cwd}); err != nil {
							t.Fatal(err)
						}
					}
					pass(t, d, st, Plan{Procs: true})
					var live []model.Session
					for _, s := range sessions(t, st) {
						if s.EndedAt == nil {
							live = append(live, s)
						}
					}
					if parent == "reused" || parent == "missing-start" {
						if len(live) == 0 || live[0].PID != 101 || live[0].ExternalID == "hook-only" {
							t.Fatalf("unreliable identity retired a live process: %+v", live)
						}
					} else if len(live) != 1 || live[0].ExternalID != "hook-only" || live[0].PID != 101 || live[0].Hosted || !live[0].ProcStart.Equal(p.procs[0].StartTime) {
						t.Fatalf("pass %d: hook-only conversation duplicated: %+v", i, live)
					}
				}
			})
		}
	}
}

func TestMissingAgentMetadataStillShowsLiveProcess(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "copilot"} {
		t.Run(agent, func(t *testing.T) {
			d, st, p, a := setup(t)
			a.obs = nil
			p.procs[0].Cmdline = agent
			p.procs[0].Argv = []string{agent}
			pass(t, d, st, Plan{Procs: true})
			ss := sessions(t, st)
			if len(ss) != 1 || ss[0].PID != p.procs[0].PID || ss[0].Adapter != agent || ss[0].Label != "unknown" || ss[0].EndedAt != nil {
				t.Fatalf("live %s with no session metadata: %+v", agent, ss)
			}
			cfg := d.Cfg.Adapters[agent]
			cfg.Enabled = false
			d.Cfg.Adapters[agent] = cfg
			p.procs = append(p.procs, model.Proc{PID: 102, Cmdline: agent, Argv: []string{agent}, StartTime: d.now().Add(-time.Minute)})
			pass(t, d, st, Plan{Procs: true})
			if got := len(sessions(t, st)); got != 1 {
				t.Fatalf("disabled adapter created a new process session: %d", got)
			}
		})
	}
}

func TestCopilotLauncherChainShowsOneSessionAndRetiresAlias(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	start := d.now().Add(-time.Hour)
	p.procs = []model.Proc{
		{PID: 10, StartTime: start, Cmdline: "copilot", Exe: `C:\Program Files\Microsoft VS Code\bin\copilot-shim\copilot.exe`},
		{PID: 11, PPID: 10, StartTime: start.Add(time.Second), Cmdline: "copilot", Exe: `C:\Users\example\scoop\shims\copilot.exe`},
	}
	pass(t, d, st, Plan{Procs: true})
	p.procs = append(p.procs, model.Proc{PID: 12, PPID: 11, StartTime: start.Add(2 * time.Second), Cmdline: "copilot", Exe: `C:\Users\example\scoop\apps\copilot-cli\current\copilot.exe`})
	pass(t, d, st, Plan{Procs: true})
	var live []model.Session
	for _, s := range sessions(t, st) {
		if s.EndedAt == nil {
			live = append(live, s)
		}
	}
	if len(live) != 1 || live[0].PID != 12 {
		t.Fatalf("launcher aliases are not separate conversations: %+v", live)
	}
}

func TestNativeChildAgentRemainsSeparate(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	start := d.now().Add(-time.Hour)
	p.procs = []model.Proc{
		{PID: 10, StartTime: start, Cmdline: "copilot", Exe: `C:\tools\copilot.exe`},
		{PID: 11, PPID: 10, StartTime: start.Add(time.Second), Cmdline: "copilot", Exe: `C:\tools\copilot.exe`},
	}
	pass(t, d, st, Plan{Procs: true})
	if got := len(sessions(t, st)); got != 2 {
		t.Fatalf("real child agent was collapsed into its parent: %d", got)
	}
}

func TestClaudeRegistryReplacesProcessPlaceholder(t *testing.T) {
	d, st, p, a := setup(t)
	obs := a.obs[0]
	a.obs = nil
	pass(t, d, st, Plan{Procs: true})
	if got := sessions(t, st); len(got) != 1 || got[0].PID != p.procs[0].PID {
		t.Fatalf("process placeholder missing: %+v", got)
	}
	a.obs = []model.SessionObs{obs}
	pass(t, d, st, Plan{Procs: true})
	for _, s := range sessions(t, st) {
		if s.ExternalID == "one" {
			if s.PID != p.procs[0].PID || s.EndedAt != nil || s.Label != "working" {
				t.Fatalf("registry session: %+v", s)
			}
		} else if s.EndedAt == nil || s.PID != 0 {
			t.Fatalf("placeholder still owns the live process: %+v", s)
		}
	}
}

func TestCodexRolloutClaimsProvisionalProcess(t *testing.T) {
	d, st, p, a := setup(t)
	a.name, a.obs = "codex", nil
	p.procs[0].Cmdline, p.procs[0].Argv = "codex", []string{"codex"}
	pass(t, d, st, Plan{Procs: true})
	if got := sessions(t, st); len(got) != 1 || got[0].PID != p.procs[0].PID {
		t.Fatalf("process placeholder missing: %+v", got)
	}
	a.obs = []model.SessionObs{{Adapter: "codex", ExternalID: "rollout", Cwd: p.procs[0].Cwd, StartedAt: p.procs[0].StartTime.Add(time.Second), LastActivityAt: d.now(), TurnOpen: true, Origin: "interactive"}}
	pass(t, d, st, Plan{Procs: true})
	found := false
	for _, s := range sessions(t, st) {
		if s.ExternalID == "rollout" {
			found = true
			if s.PID != p.procs[0].PID || s.EndedAt != nil || s.Label != "working" {
				t.Fatalf("rollout could not claim its real process: %+v", s)
			}
		} else if s.EndedAt == nil || s.PID != 0 {
			t.Fatalf("placeholder still owns the rollout's process: %+v", s)
		}
	}
	if !found {
		t.Fatal("rollout session missing")
	}
}

func TestCollectorResolvesWindowsHomeForAdapters(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows services use USERPROFILE rather than HOME")
	}
	d, st, _, a := setup(t)
	want := os.Getenv("USERPROFILE")
	t.Setenv("HOME", "")
	pass(t, d, st, Plan{})
	if len(a.envs) != 1 || a.envs[0].Home != want {
		t.Fatalf("adapter home=%s, want USERPROFILE %q", fmt.Sprint(a.envs), want)
	}
}

func TestCodexTUICorrectsPreviouslyHostedSession(t *testing.T) {
	d, st, p, a := setup(t)
	a.name = "codex"
	p.procs[0].Cmdline, p.procs[0].Argv = "codex", []string{"codex"}
	a.obs = []model.SessionObs{{Adapter: "codex", ExternalID: "rollout", Hosted: true, Origin: "hosted", Cwd: p.procs[0].Cwd, StartedAt: p.procs[0].StartTime.Add(time.Second), LastActivityAt: d.now(), TurnOpen: true}}
	pass(t, d, st, Plan{Procs: true})
	a.obs[0].Hosted, a.obs[0].Origin = false, "interactive"
	pass(t, d, st, Plan{Procs: true})
	var live []model.Session
	for _, s := range sessions(t, st) {
		if s.EndedAt == nil {
			live = append(live, s)
		}
	}
	if len(live) != 1 || live[0].ExternalID != "rollout" || live[0].Hosted || live[0].PID != p.procs[0].PID {
		t.Fatalf("corrected TUI metadata did not replace hosted/placeholder rows: %+v", live)
	}
}
