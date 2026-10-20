package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

type jumpRunner func(context.Context, string, ...string) ([]byte, error)

func (f jumpRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

func TestJumpCommand(t *testing.T) {
	for _, tc := range []struct {
		name, goos, tmux string
		clients          string
		jump             *model.InboxJump
		optIn            bool
		want             [][]string
		text, err        string
	}{
		{name: "tmux same server", tmux: "/tmp/socket with spaces,1,0", jump: &model.InboxJump{Kind: "tmux", Server: "/tmp/socket with spaces", Target: "dev:1.2", Command: "tmux attach"}, want: [][]string{{"tmux", "-S", "/tmp/socket with spaces", "switch-client", "-t", "dev:1.2"}, {"tmux", "-S", "/tmp/socket with spaces", "select-pane", "-t", "dev:1.2"}}},
		{name: "tmux outside no client", jump: &model.InboxJump{Kind: "tmux", Server: "/tmp/socket", Target: "dev:1.2", Command: "tmux attach"}, want: [][]string{{"tmux", "-S", "/tmp/socket", "list-clients", "-F", "#{client_activity} #{client_name} #{client_tty}"}, {"tmux", "-S", "/tmp/socket", "select-pane", "-t", "dev:1.2"}}, text: "no tmux client is attached"},
		{name: "tmux outside attached", goos: "linux", clients: "9 older /dev/ttys001\n100 newest /dev/ttys002\n20 other /dev/ttys003\n", jump: &model.InboxJump{Kind: "tmux", Server: "/tmp/socket with spaces", Target: "dev:1.2", Command: "tmux attach"}, want: [][]string{{"tmux", "-S", "/tmp/socket with spaces", "list-clients", "-F", "#{client_activity} #{client_name} #{client_tty}"}, {"tmux", "-S", "/tmp/socket with spaces", "switch-client", "-c", "newest", "-t", "dev:1.2"}, {"tmux", "-S", "/tmp/socket with spaces", "select-pane", "-t", "dev:1.2"}}, text: "tmux attach"},
		{name: "tmux different server", tmux: "/tmp/other,1,0", jump: &model.InboxJump{Kind: "tmux", Server: "/tmp/socket", Target: "dev:1.2", Command: "tmux attach"}, want: [][]string{{"tmux", "-S", "/tmp/socket", "select-pane", "-t", "dev:1.2"}}},
		{name: "Terminal", goos: "darwin", optIn: true, jump: &model.InboxJump{Kind: "terminal", App: "Terminal", Target: "/dev/ttys001", Command: "focus terminal"}, want: [][]string{{"osascript", "-e", inbox.TerminalActivateScript("Terminal"), "/dev/ttys001"}}},
		{name: "iTerm", goos: "darwin", optIn: true, jump: &model.InboxJump{Kind: "terminal", App: "iTerm2", Target: "/dev/ttys002", Command: "focus iterm"}, want: [][]string{{"osascript", "-e", inbox.TerminalActivateScript("iTerm2"), "/dev/ttys002"}}},
		{name: "terminal opt out", goos: "darwin", jump: &model.InboxJump{Kind: "terminal", App: "Terminal", Target: "/dev/ttys001", Command: "focus terminal"}, text: "focus terminal"},
		{name: "terminal linux", goos: "linux", optIn: true, jump: &model.InboxJump{Kind: "terminal", App: "Terminal", Target: "/dev/ttys001", Command: "focus terminal"}, text: "focus terminal"},
		{name: "resume", goos: "darwin", optIn: true, jump: &model.InboxJump{Kind: "resume", Command: "claude --resume 'session'", Cwd: "/project's dir"}, text: `cd -- '/project'"'"'s dir' && claude --resume 'session'`},
		{name: "no jump", err: "has no jump"},
		{name: "empty jump", jump: &model.InboxJump{}, err: "has no jump"},
		{name: "missing", err: "no longer exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readEnv(t)
			t.Setenv("TMUX", tc.tmux)
			if err := os.WriteFile(config.Path(), []byte("[inbox]\njump_terminal = "+map[bool]string{true: "true", false: "false"}[tc.optIn]+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			st := readDB(t, func(tx *store.Tx) error {
				if tc.name == "missing" {
					return nil
				}
				if err := tx.UpsertInboxItem(model.InboxItem{ID: "item ' with spaces", Kind: "waiting", TS: time.Now()}); err != nil {
					return err
				}
				return tx.SetInboxJump("item ' with spaces", tc.jump)
			})
			before, err := st.Inbox(time.Time{}, true)
			if err != nil {
				t.Fatal(err)
			}
			var calls [][]string
			r := jumpRunner(func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, append([]string{name}, args...))
				if len(args) > 2 && args[2] == "list-clients" {
					return []byte(tc.clients), nil
				}
				return nil, nil
			})
			var out bytes.Buffer
			err = (JumpCmd{ID: "item ' with spaces"}).run(&Globals{Stdout: &out}, r, tc.goos)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, tc.want) || !strings.Contains(out.String(), tc.text) {
				t.Fatalf("calls=%#v output=%q", calls, out.String())
			}
			after, err := st.Inbox(time.Time{}, true)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("item changed: %+v %+v %v", before, after, err)
			}
			if _, err := os.Stat(config.SpoolDir()); !os.IsNotExist(err) {
				t.Fatalf("jump wrote spool: %v", err)
			}
		})
	}
}

func TestJumpCLIAndRunnerFailure(t *testing.T) {
	readEnv(t)
	st := readDB(t, func(tx *store.Tx) error {
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "one", Kind: "waiting", TS: time.Now()}); err != nil {
			return err
		}
		return tx.SetInboxJump("one", &model.InboxJump{Kind: "tmux", Server: "/tmp/fake", Target: "dev:1.2", Command: "tmux attach"})
	})
	old := newInboxRunner
	t.Cleanup(func() { newInboxRunner = old })
	newInboxRunner = func() inbox.Runner {
		return jumpRunner(func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("fixture tmux failed")
		})
	}
	var out, stderr bytes.Buffer
	code := run([]string{"jump", "one"}, "test", strings.NewReader(""), &out, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "fixture tmux failed") {
		t.Fatal(code, stderr.String())
	}
	// A delayed notification still uses the stored item after the collector stops.
	if err := os.Remove(filepath.Join(config.DataDir(), "driga.lock")); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.SetInboxJump("one", &model.InboxJump{Kind: "resume", Command: "codex resume 'one'"})
	}); err != nil {
		t.Fatal(err)
	}
	readRun(t, "jump", "one")
	out.Reset()
	stderr.Reset()
	if code := run([]string{"jump", "gone"}, "test", strings.NewReader(""), &out, &stderr); code != 1 || !strings.Contains(stderr.String(), "no longer exists") {
		t.Fatal(code, stderr.String())
	}
}

func TestJumpMissingDatabaseDoesNotCollect(t *testing.T) {
	readEnv(t)
	var out, stderr bytes.Buffer
	if code := run([]string{"jump", "gone"}, "test", strings.NewReader(""), &out, &stderr); code != 1 || !strings.Contains(stderr.String(), "no longer exists") {
		t.Fatal(code, stderr.String())
	}
	if _, err := os.Stat(config.DataDir()); !os.IsNotExist(err) {
		t.Fatalf("created data dir: %v", err)
	}
}

func TestJumpDoesNotExecuteRemoteItem(t *testing.T) {
	readEnv(t)
	readDB(t, func(tx *store.Tx) error {
		return tx.ForMachine("peer").ApplyInbox(model.InboxItem{ID: "remote", Kind: "waiting", TS: time.Now(), Jump: &model.InboxJump{Kind: "tmux", Server: "/tmp/remote", Target: "dev:1.2"}})
	})
	err := (JumpCmd{ID: "remote"}).run(&Globals{}, jumpRunner(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("executed remote jump")
		return nil, nil
	}), "darwin")
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatal(err)
	}
}
