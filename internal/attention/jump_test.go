package attention

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestRunJumpOutsideTmux(t *testing.T) {
	const bundle = "/Applications/My Terminal.app"
	list := []string{"tmux", "-S", "/tmp/socket with spaces", "list-clients", "-F", "#{client_activity} #{client_name} #{client_tty}"}
	switchClient := []string{"tmux", "-S", "/tmp/socket with spaces", "switch-client", "-c", "client ' with spaces", "-t", "dev:1.2"}
	selectPane := []string{"tmux", "-S", "/tmp/socket with spaces", "select-pane", "-t", "dev:1.2"}
	openApp := []string{"open", "-a", bundle}
	procs := []model.Proc{
		{PID: 3, PPID: 2, TTY: "/dev/ttys002", Exe: "/opt/tmux"},
		{PID: 2, PPID: 1, TTY: "/dev/ttys002", Exe: "/bin/zsh"},
		{PID: 1, Exe: bundle + "/Contents/MacOS/terminal"},
		{PID: 4, TTY: "/dev/ttys001", Exe: "/Applications/Other.app/Contents/MacOS/other"},
	}
	for _, tc := range []struct {
		name, os, clients, fail string
		procs                   []model.Proc
		procErr                 bool
		want                    [][]string
		wantProcs, notice       bool
	}{
		{name: "explicit click activates without opt in", os: "darwin", procs: procs, want: [][]string{list, switchClient, selectPane, openApp}, wantProcs: true},
		{name: "linux never activates", os: "linux", want: [][]string{list, switchClient, selectPane}},
		{name: "client without tty", os: "darwin", clients: "0 client ' with spaces \n", want: [][]string{list, switchClient, selectPane}},
		{name: "no ancestor app", os: "darwin", procs: []model.Proc{{PID: 3, TTY: "/dev/ttys002", Exe: "/bin/zsh"}}, want: [][]string{list, switchClient, selectPane}, wantProcs: true},
		{name: "process lookup unavailable", os: "darwin", procErr: true, want: [][]string{list, switchClient, selectPane}, wantProcs: true},
		{name: "no clients", os: "darwin", clients: "\n", want: [][]string{list, selectPane}, notice: true},
		{name: "malformed clients", os: "darwin", clients: "bad\nNaN fake /dev/ttys001\n-1 negative /dev/ttys001\n100  /dev/ttys001\n", want: [][]string{list, selectPane}, notice: true},
		{name: "list failure", os: "darwin", fail: "list-clients", want: [][]string{list}},
		{name: "switch failure", os: "darwin", fail: "switch-client", want: [][]string{list, switchClient}},
		{name: "select failure", os: "darwin", fail: "select-pane", want: [][]string{list, switchClient, selectPane}},
		{name: "open failure", os: "darwin", fail: "open", procs: procs, want: [][]string{list, switchClient, selectPane, openApp}, wantProcs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			r := runnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, append([]string{name}, args...))
				if name == tc.fail || (len(args) > 2 && args[2] == tc.fail) {
					return nil, errors.New("fixture failure")
				}
				if len(args) > 2 && args[2] == "list-clients" {
					clients := tc.clients
					if clients == "" {
						clients = "9 older /dev/ttys001\n100 client ' with spaces /dev/ttys002\n20 other /dev/ttys003\n"
					}
					return []byte(clients), nil
				}
				return nil, nil
			})
			looked := false
			opts := JumpOptions{OS: tc.os, Procs: func(context.Context) ([]model.Proc, error) {
				looked = true
				if tc.procErr {
					return nil, errors.New("no process access")
				}
				return tc.procs, nil
			}}
			j := &model.InboxJump{Kind: "tmux", Server: "/tmp/socket with spaces", Target: "dev:1.2"}
			notice, err := RunJump(context.Background(), r, j, opts)
			if (err != nil) != (tc.fail != "") || (err != nil && !strings.Contains(err.Error(), "fixture failure")) {
				t.Fatalf("error = %v", err)
			}
			if !reflect.DeepEqual(calls, tc.want) || looked != tc.wantProcs || strings.Contains(notice, "no tmux client is attached") != tc.notice {
				t.Fatalf("calls=%#v procs=%v notice=%q", calls, looked, notice)
			}
		})
	}
}

func TestTerminalAppAncestry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		procs []model.Proc
		want  string
	}{
		{name: "parent without tty", procs: []model.Proc{{PID: 3, PPID: 2, TTY: "/dev/ttys002"}, {PID: 2, PPID: 1}, {PID: 1, Exe: "/Applications/Terminal.app/Contents/MacOS/Terminal"}}, want: "/Applications/Terminal.app"},
		{name: "missing parent", procs: []model.Proc{{PID: 3, PPID: 2, TTY: "/dev/ttys002"}}},
		{name: "cycle", procs: []model.Proc{{PID: 3, PPID: 2, TTY: "/dev/ttys002"}, {PID: 2, PPID: 3}}},
		{name: "wrong tty", procs: []model.Proc{{PID: 3, TTY: "/dev/ttys001", Exe: "/Applications/Wrong.app/Contents/MacOS/terminal"}}},
		{name: "bundle suffix must be a path component", procs: []model.Proc{{PID: 3, TTY: "/dev/ttys002", Exe: "/opt/fake.app-helper/bin/terminal"}}},
		{name: "relative executable", procs: []model.Proc{{PID: 3, TTY: "/dev/ttys002", Exe: "Fake.app/Contents/MacOS/terminal"}}},
		{name: "empty tty", procs: []model.Proc{{PID: 3, Exe: "/Applications/Wrong.app/Contents/MacOS/terminal"}}},
		{name: "zombie", procs: []model.Proc{{PID: 3, Zombie: true, TTY: "/dev/ttys002", Exe: "/Applications/Wrong.app/Contents/MacOS/terminal"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tty := "/dev/ttys002"
			if tc.name == "empty tty" {
				tty = ""
			}
			if got := terminalApp(tc.procs, tty); got != tc.want {
				t.Fatalf("app=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRunJumpFromDifferentTmuxServer(t *testing.T) {
	var calls [][]string
	r := runnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(args) > 2 && args[2] == "list-clients" {
			return []byte("100 attached /dev/ttys002\n"), nil
		}
		return nil, nil
	})
	j := &model.InboxJump{Kind: "tmux", Server: "/target/socket", Target: "dev:1.2"}
	_, err := RunJump(context.Background(), r, j, JumpOptions{OS: "linux", Tmux: "/other/socket,1,0"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"tmux", "-S", j.Server, "list-clients", "-F", "#{client_activity} #{client_name} #{client_tty}"}, {"tmux", "-S", j.Server, "switch-client", "-c", "attached", "-t", j.Target}, {"tmux", "-S", j.Server, "select-pane", "-t", j.Target}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("destination client was not selected: %v", calls)
	}
}
