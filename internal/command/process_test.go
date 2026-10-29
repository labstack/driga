package command

import (
	"context"
	"os"
	"os/user"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

type processPlatform struct {
	platform.Platform
	procs []model.Proc
	err   error
}

func (f processPlatform) Procs(context.Context) ([]model.Proc, error) { return f.procs, f.err }
func ownedProc(pid int32, name string) model.Proc {
	p := model.Proc{PID: pid, PPID: 2, StartTime: time.Unix(100, 123456000), Exe: "/fixture/" + name, Argv: []string{name, "unique fixture"}, Cmdline: name + " unique fixture", OwnerKnown: true, OwnerUID: uint32(os.Getuid())}
	if runtime.GOOS == "windows" {
		u, _ := user.Current()
		p.OwnerSID = u.Uid
	}
	return p
}
func resolveTargets(t *testing.T, args []string, ps, protected []model.Proc) ([]model.Proc, error) {
	t.Helper()
	p, e := Parse(Request{Argv: args})
	if e != nil {
		return nil, e
	}
	return ResolveProcessTargets(context.Background(), processPlatform{procs: ps}, p, protected)
}

func TestProcessCommandSnapshot(t *testing.T) {
	ps := []model.Proc{ownedProc(400001, "dummy"), ownedProc(400002, "dummy"), ownedProc(400003, "other")}
	for _, args := range [][]string{{"kill", "400001", "400002", "400001"}, {"kill", "-TERM", "400001", "400002"}, {"pkill", "-f", "^dummy unique fixture$"}, {"killall", "dummy"}, {"taskkill", "/PID", "400001", "/PID", "400002"}, {"taskkill", "/IM", "dummy"}, {"Stop-Process", "-Id", "400001,400002"}, {"Stop-Process", "-Name", "dummy"}} {
		got, e := resolveTargets(t, args, ps, nil)
		if e != nil {
			t.Fatalf("%v: %v", args, e)
		}
		if len(got) != 2 || got[0].PID != 400001 || got[1].PID != 400002 {
			t.Fatalf("%v: %#v", args, got)
		}
		got[0].Argv[0] = "mutated"
		if ps[0].Argv[0] != "dummy" {
			t.Fatal("snapshot aliases collector")
		}
	}
	for _, args := range [][]string{{"kill", "-9", "400001"}, {"kill", "-0", "400001"}, {"kill", "-1"}, {"kill", "0"}, {"kill", "-400001"}, {"kill", "--", "-2"}, {"kill", "-s", "BOGUS", "400001"}, {"pkill", "-P", "2"}, {"pkill", "-f", "["}, {"pkill", "-f", ""}, {"pkill", "-f", "a", "b"}, {"killall", "-u", "me"}, {"taskkill", "/T", "/PID", "400001"}, {"taskkill", "/F", "/PID", "400001"}, {"Stop-Process", "-InputObject", "x"}, {"Stop-Process", "-Name", "*"}, {"Stop-Process", "-Force", "-Id", "400001"}} {
		if _, e := resolveTargets(t, args, ps, nil); e == nil {
			t.Errorf("accepted %v", args)
		}
	}
	p, _ := Parse(Request{Script: "pkill -f dummy | cat"})
	if _, e := ResolveProcessTargets(context.Background(), processPlatform{procs: ps}, p, nil); e == nil {
		t.Fatal("composed host process request")
	}
	scope := runnerScope(t)
	prepared := prepareRun(t, Request{Argv: []string{"pkill", "-f", "^dummy unique fixture$"}, Cwd: scope.Project}, scope)
	bound, e := PrepareProcessTargets(context.Background(), processPlatform{procs: ps}, prepared, nil)
	if e != nil {
		t.Fatal(e)
	}
	if bound.ProcessSnapshot == nil || len(bound.ProcessSnapshot.Targets) != 2 {
		t.Fatal("missing bound snapshot")
	}
	force, e := PrepareForce(bound, ps[:1])
	if e != nil || Digest(force) == Digest(bound) || !force.ProcessSnapshot.Force || len(force.ProcessSnapshot.Targets) != 1 {
		t.Fatalf("force binding: %v", e)
	}
	if _, e = PrepareForce(bound, ps[2:]); e == nil {
		t.Fatal("force added a new target")
	}
	before := Digest(bound)
	bound.ProcessSnapshot.Targets[0].OwnerUID++
	if Digest(bound) == before {
		t.Fatal("ownership omitted from digest")
	}
}

func TestProcessProtectedTargets(t *testing.T) {
	base := ownedProc(400001, "dummy")
	for _, change := range []func(*model.Proc){func(p *model.Proc) { p.OwnerKnown = false }, func(p *model.Proc) { p.OwnerUID++; p.OwnerSID = "S-other" }, func(p *model.Proc) { p.PID = 1 }, func(p *model.Proc) { p.StartTime = time.Time{} }, func(p *model.Proc) { p.Exe = "/fixture/driga" }, func(p *model.Proc) { p.Partial = true }, func(p *model.Proc) { p.PPID = 400002 }} {
		p := base
		change(&p)
		ps := []model.Proc{p, ownedProc(400002, "chat")}
		if _, e := resolveTargets(t, []string{"killall", "dummy"}, ps, ps[1:]); e == nil {
			t.Fatalf("protected accepted: %#v", p)
		}
	}
	root := ownedProc(400002, "chat")
	child := base
	child.PPID = root.PID
	grand := ownedProc(400003, "dummy")
	grand.PPID = child.PID
	if _, e := resolveTargets(t, []string{"kill", "400003"}, []model.Proc{root, child, grand}, []model.Proc{root}); e == nil {
		t.Fatal("protected grandchild")
	}
	parent := ownedProc(400004, "dummy")
	root.PPID = parent.PID
	if _, e := resolveTargets(t, []string{"kill", "400004"}, []model.Proc{parent, root}, []model.Proc{root}); e == nil {
		t.Fatal("Chat ancestor")
	}
	p := base
	p.PID = int32(os.Getpid())
	if _, e := resolveTargets(t, []string{"killall", "dummy"}, []model.Proc{p}, nil); e == nil {
		t.Fatal("self")
	}
	if !reflect.DeepEqual(base.Argv, []string{"dummy", "unique fixture"}) {
		t.Fatal("mutated input")
	}
}
