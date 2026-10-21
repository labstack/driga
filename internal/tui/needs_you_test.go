package tui

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestNeedsYouInboxJumpAndHeader(t *testing.T) {
	f := fixture()
	f.snap.Inbox.Rows[0].Reason = "permission"
	f.snap.Inbox.Rows[0].Tool = "Bash"
	f.snap.Inbox.Rows[0].WaitingSinceAt = f.snap.ObservedAt.Add(-time.Minute)
	f.snap.Inbox.Rows[0].TS = f.snap.Inbox.Rows[0].WaitingSinceAt
	f.snap.Inbox.Rows[0].Jump = &model.InboxJump{Kind: "tmux", Target: "dev:1.2", Server: "/tmp/fake", Command: "tmux attach -t 'dev:1.2'"}
	m := setup(t, f, Options{Exec: func(args []string) error {
		if strings.Join(args, " ") != "tmux -S /tmp/fake select-pane -t dev:1.2" {
			t.Fatal(args)
		}
		return nil
	}}).(*app)
	m.tab = homeTab
	if !strings.Contains(m.header(), "1 waiting") || !strings.Contains(m.header(), "oldest 1m") {
		t.Fatal(m.header())
	}
	m.tab = 2
	m.rebuild()
	text := m.View().Content
	if !strings.Contains(text, "permission") || !strings.Contains(text, "Bash") {
		t.Fatal(text)
	}
	if cmd := m.openSelected(false); cmd != nil {
		cmd()
	}
	wantStatus := "tmux attach"
	if runtime.GOOS == "windows" {
		wantStatus = "not yet supported on Windows"
	}
	if m.detail || !strings.Contains(m.status, wantStatus) {
		t.Fatal("Enter did not offer jump", m.status)
	}
}

func TestNeedsYouJumpSwitchesOnlySameLocalServer(t *testing.T) {
	for _, remote := range []bool{false, true} {
		f := fixture()
		f.snap.LocalMachineID = "local"
		row := &f.snap.Inbox.Rows[0]
		row.MachineID = "local"
		if remote {
			row.MachineID = "peer"
		}
		row.Jump = &model.InboxJump{Kind: "tmux", Server: "/tmp/fake-socket", Target: "dev:1.2", Command: "tmux attach -t 'dev:1.2'"}
		calls := 0
		m := setup(t, f, Options{Tmux: "/tmp/fake-socket,123,0", Exec: func(args []string) error {
			calls++
			action := "switch-client"
			if calls == 2 {
				action = "select-pane"
			}
			if strings.Join(args, " ") != "tmux -S /tmp/fake-socket "+action+" -t dev:1.2" {
				t.Fatal(args)
			}
			return nil
		}}).(*app)
		m.tab = 2
		m.rebuild()
		if cmd := m.openSelected(false); cmd != nil {
			cmd()
		}
		wantCalls := 0
		if !remote && runtime.GOOS != "windows" {
			wantCalls = 2
		}
		if calls != wantCalls {
			t.Fatal(remote, calls, m.status)
		}
		if !remote && runtime.GOOS == "windows" && !strings.Contains(m.status, "not yet supported on Windows") {
			t.Fatal(m.status)
		}
	}
}
