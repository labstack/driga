package command

import (
	"bytes"
	"context"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func testSandboxNetwork(t *testing.T, s Scope) {
	t.Helper()
	// Build a standalone fixture in the project, using only the standard library.
	src := `package main
import("net";"os")
func main(){var e error;switch os.Args[1]{case "tcp","unix":var c net.Conn;c,e=net.Dial(os.Args[1],os.Args[2]);if c!=nil{c.Close()};case "dns":_,e=net.LookupHost("example.com")};if e!=nil{os.Exit(23)}}`
	source := filepath.Join(s.Project, "network.go")
	if e := os.WriteFile(source, []byte(src), 0600); e != nil {
		t.Fatal(e)
	}
	bin := filepath.Join(s.Project, "network")
	cmd := exec.Command("go", "build", "-o", bin, source)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture: %v %s", e, b)
	}
	tcp, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer tcp.Close()
	unixPath := filepath.Join(s.Project, "fixture.sock")
	sock, e := net.Listen("unix", unixPath)
	if e != nil {
		t.Fatal(e)
	}
	defer sock.Close()
	for _, tc := range []struct {
		kind, addr  string
		network, ok bool
	}{{"tcp", tcp.Addr().String(), false, false}, {"dns", "", false, false}, {"unix", unixPath, false, false}, {"tcp", tcp.Addr().String(), true, true}, {"unix", unixPath, true, false}} {
		p := prepareRun(t, Request{Argv: []string{bin, tc.kind, tc.addr}, Cwd: s.Project, Network: tc.network}, s)
		r, e := Run(context.Background(), p, &bytes.Buffer{}, &bytes.Buffer{})
		if e != nil {
			t.Fatal(e)
		}
		if (r.ExitCode == 0) != tc.ok {
			t.Fatalf("%+v: exit %d", tc, r.ExitCode)
		}
	}
}

func testSandboxDescendants(t *testing.T, s Scope) {
	t.Helper()
	source := filepath.Join(s.Project, "tree.go")
	bin := filepath.Join(s.Project, "tree")
	src := `package main
import("fmt";"os";"os/exec";"time")
func main(){level:="parent";if len(os.Args)>1{level=os.Args[1]};if level!="grandchild" {next:="child";if level=="child"{next="grandchild"};cmd:=exec.Command(os.Args[0],next);cmd.Stdout=os.Stdout;cmd.Stderr=os.Stderr;if e:=cmd.Start();e!=nil{panic(e)}};fmt.Printf("%s %d\n",level,os.Getpid());for{time.Sleep(time.Hour)}}`
	if e := os.WriteFile(source, []byte(src), 0600); e != nil {
		t.Fatal(e)
	}
	if b, e := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); e != nil {
		t.Fatalf("tree fixture: %v %s", e, b)
	}
	p := prepareRun(t, Request{Argv: []string{bin}, Cwd: s.Project, TimeoutSeconds: 1}, s)
	var out, stderr bytes.Buffer
	r, e := Run(context.Background(), p, &out, &stderr)
	if e == nil || !r.TimedOut {
		t.Fatalf("tree timeout: %#v %v %s", r, e, stderr.String())
	}
	owned := map[int32]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("fixture barrier: %q", out.String())
		}
		n, e := strconv.ParseInt(fields[1], 10, 32)
		if e != nil || n <= 1 {
			t.Fatal("bad fixture PID")
		}
		owned[int32(n)] = true
	}
	if len(owned) != 3 {
		t.Fatalf("parent/child/grandchild did not all start: %q", out.String())
	}
	executable, e := filepath.EvalSymlinks(bin)
	if e != nil {
		t.Fatal(e)
	}
	plat := platform.New()
	ps, e := plat.Procs(context.Background())
	if e != nil {
		t.Fatalf("inspect owned descendants: %v", e)
	}
	var leaked []model.Proc
	for _, proc := range ps {
		// bwrap's printed PIDs belong to its private PID namespace. Never
		// interpret them as host identities. This generated executable is
		// unique to this fixture and is the only code its descendants run.
		if proc.Exe == executable && !proc.Zombie {
			leaked = append(leaked, proc)
		}
	}
	if len(leaked) > 0 {
		// Only exact identities of our own unique fixture executable may be cleaned.
		for _, p := range leaked {
			child, e := os.FindProcess(int(p.PID))
			if e != nil {
				continue
			}
			if start, e := platform.ProcessStart(p.PID); e == nil && start.Equal(p.StartTime) {
				_ = child.Kill()
			}
			_ = child.Release()
		}
		t.Fatalf("timeout left %d owned descendants", len(leaked))
	}
}

func TestCommandSandboxIntegration(t *testing.T) {
	realSandbox(t)
	if runtime.GOOS == "linux" {
		if out, err := exec.Command("bwrap", "--version").CombinedOutput(); err == nil {
			t.Logf("backend version: %s", strings.TrimSpace(string(out)))
		} else {
			t.Fatalf("backend version: %v %s", err, out)
		}
	}
	t.Run("boundaries", func(t *testing.T) {
		TestCommandRunnerBoundaries(t)
		t.Log("verified project/scratch writes; denied outside writes, symlink escapes, protected-store reads and host signalling; stripped secret environment")
	})
	t.Run("execution-hooks", func(t *testing.T) {
		testSandboxExecutionHooks(t)
	})
	t.Run("network", func(t *testing.T) {
		TestCommandRunnerNetwork(t)
		t.Log("verified TCP with explicit grant; denied default TCP/DNS and Unix sockets even with a network grant")
	})
	t.Run("timeout", func(t *testing.T) {
		TestCommandRunnerTimeout(t)
		t.Log("verified stdout/stderr, exit status and timeout cleanup of fixture parent/child/grandchild")
	})
}
