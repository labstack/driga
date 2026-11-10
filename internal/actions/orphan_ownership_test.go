package actions

import (
	"context"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
)

type ownershipPlatform struct {
	platform.Platform // Unimplemented operations cannot touch native processes.
	procs             []model.Proc
	cwds              map[int32]string
}

func (p ownershipPlatform) Procs(context.Context) ([]model.Proc, error) {
	return append([]model.Proc(nil), p.procs...), nil
}
func (p ownershipPlatform) Cwds(_ context.Context, pids []int32) (map[int32]string, error) {
	cwds := map[int32]string{}
	for _, pid := range pids {
		cwds[pid] = p.cwds[pid]
	}
	return cwds, nil
}

type nativeOwnershipActionPlatform struct{ outputFailurePlatform }

func (p *nativeOwnershipActionPlatform) Cwds(_ context.Context, pids []int32) (map[int32]string, error) {
	cwds := map[int32]string{}
	for _, proc := range p.procs {
		cwds[proc.PID] = proc.Cwd
	}
	return (ownershipPlatform{cwds: cwds}).Cwds(context.Background(), pids)
}

func TestExecuteRefusesNativeAgentOwnershipBeforeSignals(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "builtin", true: "extra signature"}[custom], func(t *testing.T) {
			t.Setenv("DRIGA_DATA_DIR", t.TempDir())
			st, err := store.Open("", store.Memory)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			project := t.TempDir()
			root := model.Proc{PID: 123456789, PPID: 1, StartTime: time.Now().Add(-time.Hour), OwnerKnown: true, OwnerUID: uint32(os.Getuid()), Exe: "/fixture/helper", Cmdline: "helper", Argv: []string{"helper"}, Cwd: filepath.Join(project, "nested")}
			if runtime.GOOS == "windows" {
				u, err := user.Current()
				if err != nil {
					t.Fatal(err)
				}
				root.OwnerSID = u.Uid
			}
			agent := root
			agent.PID, agent.Exe, agent.Cmdline, agent.Argv, agent.Cwd = root.PID+1, "/tools/codex", "codex", []string{"codex"}, project
			cfg := config.Config{}
			if custom {
				path := filepath.Join(t.TempDir(), "signatures.toml")
				if err := os.WriteFile(path, []byte("[[signature]]\nname = 'custom-agent'\nagent = 'custom'\nrole = 'interactive'\nmatch = ['^fixture-agent$']\n"), 0600); err != nil {
					t.Fatal(err)
				}
				cfg.ExtraSignatures = []string{path}
				agent.Exe, agent.Cmdline, agent.Argv = "/tools/fixture-agent", "fixture-agent", []string{"fixture-agent"}
			}
			p := &nativeOwnershipActionPlatform{outputFailurePlatform{procs: []model.Proc{root}}}
			env := Environment{Stdout: io.Discard, Platform: func() platform.Platform { return p }, VerifyWindow: time.Nanosecond, Confirm: func(string) error {
				// The live agent arrives after the displayed frame and consent.
				p.procs = append(p.procs, agent)
				return nil
			}, OpenReader: func(context.Context) (*store.Store, string, func(), error) { return st, "", func() {}, nil }}
			report, err := (KillOptions{}).Execute(context.Background(), env, cfg, []model.Proc{root}, []model.Orphan{{ID: "detached", RootPID: root.PID, StartTime: root.StartTime, PIDs: []int32{root.PID}, Verified: true}})
			if err == nil || !strings.Contains(err.Error(), "live agent PID") || len(report.Outcomes) != 1 || report.Outcomes[0].Status != "refused" || report.Outcomes[0].Result != nil || p.aliveCalls != 0 {
				t.Fatalf("native ownership reached shutdown: report=%+v err=%v alive calls=%d", report, err, p.aliveCalls)
			}
		})
	}
}

func TestOrphanOwnershipReloadsSessionsAndNativeDirectories(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := Environment{OpenReader: func(context.Context) (*store.Store, string, func(), error) { return st, "", func() {}, nil }}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	p := ownershipPlatform{procs: []model.Proc{{PID: 10, PPID: 1, StartTime: start}, {PID: 90, StartTime: start}}, cwds: map[int32]string{10: filepath.Join(repo, "pkg")}}
	o := model.Orphan{ID: "10:1", RootPID: 10, StartTime: start, Cwd: "/old-displayed-dir", Verified: true}
	if err := verifyOrphanOwnership(ctx, g, p, o, signatures.Builtin()); err != nil {
		t.Fatal(err)
	}
	missing := p
	missing.cwds = nil
	if err := verifyOrphanOwnership(ctx, g, missing, o, signatures.Builtin()); err == nil {
		t.Fatal("missing native tree directory authorized kill with no stored sessions")
	}
	// A session appeared after the candidate was displayed. Even a previously
	// verified idle tree must not be automatically stopped below its directory.
	err = st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{Label: "working", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "new-session", PID: 90, ProcStart: start, Cwd: repo}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyOrphanOwnership(ctx, g, p, o, signatures.Builtin()); err == nil || !strings.Contains(err.Error(), "ownership requires review") {
		t.Fatalf("fresh live session ignored: %v", err)
	}
	// A failed directory observation must not turn the same overlap into proof
	// that this tree is disconnected.
	p.cwds = nil
	if err := verifyOrphanOwnership(ctx, g, p, o, signatures.Builtin()); err == nil {
		t.Fatal("missing current cwd authorized kill")
	}
}

func TestOrphanOwnershipCanonicalizesHookSessionDirectory(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	real, alias := filepath.Join(base, "real"), filepath.Join(base, "alias")
	if err := os.MkdirAll(filepath.Join(real, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	st, err := store.Open(filepath.Join(base, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// Hook-only sessions can retain the caller's logical path before an adapter
	// observation canonicalizes it. Native process cwd inspection sees the target.
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{Label: "working", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "hook-only", PID: 90, ProcStart: start, Cwd: alias}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	g := Environment{OpenReader: func(context.Context) (*store.Store, string, func(), error) { return st, "", func() {}, nil }}
	p := ownershipPlatform{procs: []model.Proc{{PID: 10, PPID: 1, StartTime: start}, {PID: 90, StartTime: start}}, cwds: map[int32]string{10: filepath.Join(real, "pkg")}}
	o := model.Orphan{ID: "10:1", RootPID: 10, StartTime: start, Verified: true}
	if err := verifyOrphanOwnership(ctx, g, p, o, signatures.Builtin()); err == nil || !strings.Contains(err.Error(), "ownership requires review") {
		t.Fatalf("hook session alias authorized shutdown: %v", err)
	}
}

func TestOrphanOwnershipChecksNativeAgentsWithoutSessionRecords(t *testing.T) {
	for _, scenario := range []string{"overlap", "ancestor", "contained agent", "missing agent cwd", "relative agent cwd", "unknown agent owner", "disjoint", "foreign owner", "host only", "custom signature", "native directory alias"} {
		t.Run(scenario, func(t *testing.T) {
			st, err := store.Open("", store.Memory)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			g := Environment{OpenReader: func(context.Context) (*store.Store, string, func(), error) { return st, "", func() {}, nil }}
			start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			root := model.Proc{PID: 10, PPID: 1, StartTime: start, OwnerKnown: true, OwnerUID: 123, OwnerSID: "fixture-user"}
			agent := model.Proc{PID: 90, PPID: 2, StartTime: start, OwnerKnown: true, OwnerUID: 123, OwnerSID: "fixture-user", Exe: "/tools/codex", Cmdline: "codex", Argv: []string{"codex"}}
			project := filepath.Join(t.TempDir(), "project")
			cwds := map[int32]string{10: filepath.Join(project, "nested"), 90: project}
			var extra []string
			switch scenario {
			case "ancestor":
				root.PPID = agent.PID
				cwds[90] = t.TempDir()
			case "contained agent":
				agent.PPID = root.PID
				cwds[90] = t.TempDir()
			case "missing agent cwd":
				delete(cwds, 90)
			case "relative agent cwd":
				cwds[90] = "relative/project"
			case "unknown agent owner":
				agent.OwnerKnown = false
			case "disjoint":
				cwds[90] = project + "-two"
			case "foreign owner":
				agent.OwnerUID, agent.OwnerSID = 124, "another-user"
			case "host only":
				agent.Cmdline, agent.Argv = "codex app-server", []string{"codex", "app-server"}
			case "native directory alias":
				if err := os.MkdirAll(cwds[10], 0700); err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(project, alias); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				cwds[90] = alias
			case "custom signature":
				path := filepath.Join(t.TempDir(), "signatures.toml")
				if err := os.WriteFile(path, []byte("[[signature]]\nname = 'custom-agent'\nagent = 'custom'\nrole = 'interactive'\nmatch = ['^fixture-agent$']\n"), 0600); err != nil {
					t.Fatal(err)
				}
				extra = []string{path}
				agent.Exe, agent.Cmdline, agent.Argv = "/tools/fixture-agent", "fixture-agent", []string{"fixture-agent"}
			}
			sigs, err := signatures.Load(extra)
			if err != nil {
				t.Fatal(err)
			}
			p := ownershipPlatform{procs: []model.Proc{root, agent}, cwds: cwds}
			o := model.Orphan{ID: "detached", RootPID: 10, StartTime: start, Verified: true}
			err = verifyOrphanOwnership(context.Background(), g, p, o, sigs)
			wantRefused := scenario != "disjoint" && scenario != "foreign owner" && scenario != "host only"
			if (err != nil) != wantRefused {
				t.Fatalf("native ownership refusal=%t, want %t: %v", err != nil, wantRefused, err)
			}
		})
	}
}
