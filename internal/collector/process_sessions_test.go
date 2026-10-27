package collector

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

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
