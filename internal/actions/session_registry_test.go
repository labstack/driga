package actions

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
)

type registrySwitchSource struct{ obs model.SessionObs }

func (*registrySwitchSource) Name() string             { return "claude" }
func (*registrySwitchSource) TestedVersions() []string { return nil }
func (s *registrySwitchSource) Collect(context.Context, adapter.Env) ([]model.SessionObs, []adapter.Warning, error) {
	return []model.SessionObs{s.obs}, nil, nil
}

type registrySwitchPlatform struct {
	platform.Platform // Any unexpected operation fails instead of reaching the OS.
	proc              model.Proc
}

func (p registrySwitchPlatform) Procs(context.Context) ([]model.Proc, error) {
	return []model.Proc{p.proc}, nil
}
func (p registrySwitchPlatform) Cwds(context.Context, []int32) (map[int32]string, error) {
	return nil, nil
}
func (p registrySwitchPlatform) Alive(pid int32, start time.Time) bool {
	return pid == p.proc.PID && start.Equal(p.proc.StartTime)
}

func TestKillSessionRejectsSuppressedRegistryReplacement(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DRIGA_DATA_DIR", t.TempDir())
	t.Setenv("DRIGA_SPOOL_DIR", t.TempDir())
	configPath := filepath.Join(home, "config.toml")
	if err := os.WriteFile(configPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_CONFIG", configPath)
	st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	p := registrySwitchPlatform{proc: model.Proc{PID: 1036, StartTime: start, Agent: "claude", Role: "helper"}}
	source := &registrySwitchSource{obs: model.SessionObs{Adapter: "claude", ExternalID: "A", Origin: "background", PID: 1036, ProcStart: start, RegistryStatus: "busy", LastActivityAt: now}}
	d := collector.Deps{Cfg: config.Default(), Plat: p, Adapters: []adapter.Adapter{source}, SpoolDir: config.SpoolDir(), Now: func() time.Time { return now }, State: collector.NewState()}
	if _, err := collector.Pass(context.Background(), d, st, collector.Plan{Procs: true}); err != nil {
		t.Fatal(err)
	}
	ss, err := st.Sessions(store.SessionFilter{})
	if err != nil || len(ss) != 1 {
		t.Fatalf("initial session: %+v, %v", ss, err)
	}
	id := ss[0].ID
	source.obs.ExternalID, source.obs.RegistryStatus = "B", "idle"
	for range 2 {
		d.State = collector.NewState()
		if _, err := collector.Pass(context.Background(), d, st, collector.Plan{Procs: true}); err != nil {
			t.Fatal(err)
		}
		// A tripwire prevents even a broken collector from reaching real
		// process inspection or signaling through the action under test.
		err := KillSession(context.Background(), Environment{Stdout: io.Discard, Platform: func() platform.Platform {
			t.Fatal("kill action targeted B's PID as session A")
			return p
		}}, id, false)
		if err == nil || err.Error() != "session process identity unavailable" {
			t.Fatalf("kill did not reject A's retired identity: %v", err)
		}
	}
}
