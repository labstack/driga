package attention

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
)

func TestLinuxNotifierExactArguments(t *testing.T) {
	for _, kind := range []string{"waiting", "finished"} {
		sound := "message-new-instant"
		if kind == "finished" {
			sound = "complete"
		}
		r := runnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
			want := []string{"notify-send", "--app-name=Driga", "--hint=string:category:" + kind, "--hint=string:sound-name:" + sound, "title", "body"}
			if !reflect.DeepEqual(append([]string{name}, args...), want) {
				t.Fatal(name, args)
			}
			return nil, nil
		})
		d := DesktopNotifier{Runner: r, OS: "linux"}
		if err := d.Notify(context.Background(), Notification{Kind: kind, Title: "title", Body: "body"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAlertTextCapturePolicies(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	c := config.Default().Inbox
	c.Alerts = "all"
	for _, local := range []string{"none", "titles", "snippets"} {
		for _, share := range []string{"none", "titles", "snippets"} {
			for _, kind := range []string{"waiting", "finished"} {
				t.Run(local+"/"+string(share)+"/"+kind, func(t *testing.T) {
					i := model.InboxItem{ID: "one", MachineID: "peer", SessionID: 1, Kind: kind, TS: at, Reason: "permission", Tool: "Bash", Detail: "private command", Summary: "private summary"}
					s := model.Session{MachineID: "peer", ID: 1, SessionObs: model.SessionObs{Title: "Session title", Cwd: "/code/project"}}
					// Model the existing sender policy before applying local capture.
					i = Policy(i, string(share))
					if share == "none" {
						s.Title = ""
					}
					got := PlanAlerts([]model.InboxItem{i}, []model.Session{s}, c, local, at, map[string]time.Time{}, map[string]time.Time{})
					label := "project"
					if local != "none" && share != "none" {
						label = "Session title"
					}
					reason, title := "permission", "Needs you"
					if kind == "finished" {
						reason, title = "turn finished", "Finished"
					}
					if local != "none" && share != "none" {
						reason += " (Bash)"
					}
					want := label + " · " + reason
					if local == "snippets" && share == "snippets" {
						want += " · private command"
					}
					if len(got) != 1 || got[0].Title != title || got[0].Body != want || got[0].ItemID != "one" {
						t.Fatalf("got %+v; want %q / %q", got, title, want)
					}
				})
			}
		}
	}
}

func TestAlertTextSessionIdentityAndFallback(t *testing.T) {
	at := time.Unix(100, 0)
	ss := []model.Session{{MachineID: "other", ID: 1, SessionObs: model.SessionObs{Title: "wrong"}}, {MachineID: "local", ID: 1, SessionObs: model.SessionObs{Cwd: "/code/driga"}}}
	items := []model.InboxItem{{ID: "a", MachineID: "local", SessionID: 1, Kind: "waiting", TS: at, Reason: "agent_needs_input"}}
	n := PlanAlerts(items, ss, config.Default().Inbox, "titles", at, map[string]time.Time{}, map[string]time.Time{})
	if len(n) != 1 || n[0].Body != "driga · agent needs input" {
		t.Fatal(n)
	}
}

func TestAlertSnippetsAreScrubbedAndBounded(t *testing.T) {
	at := time.Unix(100, 0)
	i := model.InboxItem{ID: "one", SessionID: 1, Kind: "waiting", TS: at, Reason: "permission", Tool: "Bash", Detail: "ghp_fixturesecret\n" + strings.Repeat("界", 250)}
	s := model.Session{ID: 1, SessionObs: model.SessionObs{Title: "Project\n title", LastPrompt: "never use the prompt"}}
	n := PlanAlerts([]model.InboxItem{i}, []model.Session{s}, config.Default().Inbox, "snippets", at, map[string]time.Time{}, map[string]time.Time{})
	want := "Project title · permission (Bash) · [redacted] " + strings.Repeat("界", 188) + "…"
	if len(n) != 1 || n[0].Body != want {
		t.Fatalf("notes=%+v; want %q", n, want)
	}
}
