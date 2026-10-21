package inbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
)

type runnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, n string, a ...string) ([]byte, error) {
	return f(ctx, n, a...)
}

func TestAgentsBudgetAndFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		fail, slow bool
	}{
		{name: "blocked", data: `[{"id":"a","state":"blocked","waitingFor":"permission","cwd":"/repo"},{"sessionId":"b","state":"blocked","waitingFor":"question"},{"id":"c","state":"running"}]`},
		{name: "missing", fail: true}, {name: "slow", slow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runnerFunc(func(ctx context.Context, n string, args ...string) ([]byte, error) {
				if n != "claude" || strings.Join(args, " ") != "agents --json" {
					t.Fatal(n, args)
				}
				if tc.slow {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				if tc.fail {
					return nil, errors.New("missing")
				}
				return []byte(tc.data), nil
			})
			start := time.Now()
			got, err := PollAgents(context.Background(), r, 10*time.Millisecond)
			if tc.fail || tc.slow {
				if err == nil || len(got) != 0 {
					t.Fatal(got, err)
				}
			} else if err != nil || len(got) != 3 || got[0].ID != "a" || got[0].Reason != "permission" {
				t.Fatal(got, err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("over budget")
			}
		})
	}
}

func TestJumpProvenPaneAndFallback(t *testing.T) {
	s := model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "s'$(x)", Cwd: "/repo"}}
	r := runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("/dev/ttys001 dev:1.2\n/dev/ttys002 other:0.1\n"), nil
	})
	panes := ReadPanes(context.Background(), r)
	j := ResolveJump(s, "/dev/ttys001", panes)
	if j == nil || j.Kind != "tmux" || j.Target != "dev:1.2" {
		t.Fatal(j)
	}
	j = ResolveJump(s, "/dev/ttys003", panes)
	if j.Kind != "resume" || j.URL != "" || !strings.Contains(j.Command, "claude --resume '") {
		t.Fatal(j)
	}
}

func TestTerminalJumpNeedsProof(t *testing.T) {
	for _, app := range []string{"Terminal", "iTerm2", "unproven", ""} {
		r := runnerFunc(func(context.Context, string, ...string) ([]byte, error) { return []byte(app), nil })
		got := TerminalJump(context.Background(), r, "/dev/ttys001")
		if (got != nil) != (app == "Terminal" || app == "iTerm2") {
			t.Fatal(app, got)
		}
	}
}

func TestNotifierUsesArgvAndDistinctLinuxSounds(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		var calls [][]string
		r := runnerFunc(func(_ context.Context, n string, a ...string) ([]byte, error) {
			calls = append(calls, append([]string{n}, a...))
			return nil, nil
		})
		d := DesktopNotifier{Runner: r, OS: goos, Lookup: func() string { return "" }}
		for _, n := range []Notification{{Kind: "waiting", Title: "Needs input", Body: `quoted " message`, Sound: "Glass"}, {Kind: "finished", Title: "Done", Body: "done", Sound: "Pop"}} {
			if err := d.Notify(context.Background(), n); err != nil {
				t.Fatal(err)
			}
		}
		if len(calls) != 2 {
			t.Fatal(calls)
		}
		if goos == "linux" && (!strings.Contains(strings.Join(calls[0], " "), "sound-name:") || strings.Join(calls[0], " ") == strings.Join(calls[1], " ")) {
			t.Fatal("missing distinct sound hints", calls)
		}
		if goos == "darwin" && strings.Contains(calls[0][2], `quoted " message`) {
			t.Fatal("body interpolated into AppleScript")
		}
	}
}

func TestAlertPolicyThrottleSummaryAndKinds(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	cfg := config.Default()
	items := []model.InboxItem{{ID: "a", SessionID: 1, Kind: "waiting", Reason: "permission", TS: at, WaitingSinceAt: at, Detail: "private command", Tool: "Bash"}, {ID: "b", SessionID: 2, Kind: "waiting", TS: at, WaitingSinceAt: at}, {ID: "done", SessionID: 3, Kind: "finished", TS: at}}
	seen := map[string]time.Time{}
	sessions := map[string]time.Time{}
	notes := PlanAlerts(items, nil, cfg.Inbox, cfg.CaptureText, at, seen, sessions)
	if len(notes) != 1 || notes[0].Kind != "waiting" || !strings.Contains(notes[0].Body, "2 sessions waiting") || strings.Contains(notes[0].Body, "private") {
		t.Fatal(notes)
	}
	if more := PlanAlerts(items, nil, cfg.Inbox, cfg.CaptureText, at.Add(time.Second), seen, sessions); len(more) != 0 {
		t.Fatal(more)
	}
	items[0].ID = "again"
	items = items[:1]
	if more := PlanAlerts(items, nil, cfg.Inbox, cfg.CaptureText, at.Add(time.Second), seen, sessions); len(more) != 0 {
		t.Fatal("not throttled", more)
	}
	cfg.Inbox.Alerts = "all"
	notes = PlanAlerts([]model.InboxItem{{ID: "finished", SessionID: 3, Kind: "finished", TS: at}}, nil, cfg.Inbox, "titles", at, seen, sessions)
	if len(notes) != 1 || notes[0].Kind != "finished" || notes[0].Title == "Needs input" {
		t.Fatal(notes)
	}
}

// alert_after is a delay: a wait alerts only while it is still open that long
// after it began, so a prompt answered at the terminal never pings. Earlier
// passes must not mark it seen, or the later alert would be lost.
func TestAlertAfterDelaysUntilWaitLasts(t *testing.T) {
	c := config.Default().Inbox
	c.Alerts, c.AlertAfter = "waiting", 2*time.Minute
	at := time.Unix(100, 0).UTC()
	seen, last := map[string]time.Time{}, map[string]time.Time{}
	wait := []model.InboxItem{{ID: "w", SessionID: 1, Kind: "waiting", TS: at, WaitingSinceAt: at, Reason: "permission"}}
	for _, d := range []time.Duration{0, time.Minute, 119 * time.Second} {
		if notes := PlanAlerts(wait, nil, c, "titles", at.Add(d), seen, last); len(notes) != 0 {
			t.Fatalf("alerted after %v: %v", d, notes)
		}
	}
	if notes := PlanAlerts(wait, nil, c, "titles", at.Add(2*time.Minute), seen, last); len(notes) != 1 || notes[0].Body != "Session · permission" {
		t.Fatalf("no alert once the wait lasted: %v", notes)
	}
	if notes := PlanAlerts(wait, nil, c, "titles", at.Add(10*time.Minute), seen, last); len(notes) != 0 {
		t.Fatal("alerted twice", notes)
	}
	end := at.Add(30 * time.Second)
	answered := []model.InboxItem{{ID: "a", SessionID: 2, Kind: "waiting", TS: at, WaitingSinceAt: at, EndedAt: &end}}
	if notes := PlanAlerts(answered, nil, c, "titles", at.Add(5*time.Minute), seen, last); len(notes) != 0 {
		t.Fatal("alerted an answered wait", notes)
	}
}

func TestAlertThrottleCoversBothKinds(t *testing.T) {
	c := config.Default().Inbox
	c.Alerts = "all"
	at := time.Unix(100, 0).UTC()
	seen, last := map[string]time.Time{}, map[string]time.Time{}
	first := PlanAlerts([]model.InboxItem{{ID: "wait", SessionID: 1, Kind: "waiting", TS: at}}, nil, c, "titles", at, seen, last)
	next := PlanAlerts([]model.InboxItem{{ID: "done", SessionID: 1, Kind: "finished", TS: at.Add(time.Second)}}, nil, c, "titles", at.Add(time.Second), seen, last)
	if len(first) != 1 || len(next) != 0 {
		t.Fatal(first, next)
	}
}
