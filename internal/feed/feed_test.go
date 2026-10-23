package feed

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kballard/go-shellquote"

	"github.com/labstack/driga/internal/model"
)

func TestShareTextGating(t *testing.T) {
	s := model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "one", Title: "a title", LastPrompt: "a prompt"}}
	for _, tc := range []struct {
		share                  ShareText
		capture, title, prompt string
	}{
		{Titles, "snippets", "a title", ""}, {None, "snippets", "", ""},
		{Snippets, "titles", "a title", ""}, {Snippets, "snippets", "a title", "a prompt"},
	} {
		_, b := Encode(Session, s, EffectiveShare(tc.share, tc.capture))
		var got model.Session
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.Title != tc.title || got.LastPrompt != tc.prompt {
			t.Fatalf("%+v: %s", tc, b)
		}
	}
	for _, share := range []ShareText{None, Titles} {
		_, b := Encode(Inbox, model.InboxItem{Kind: "finished", Summary: "private answer"}, share)
		if strings.Contains(string(b), "private answer") || !strings.Contains(string(b), "turn finished") {
			t.Fatalf("inbox: %s", b)
		}
		_, b = Encode(Handoff, HandoffRow{Text: "private answer"}, share)
		if b != nil {
			t.Fatalf("handoff shared: %s", b)
		}
	}
	_, b := Encode(Worktree, model.Worktree{LastCommitSubject: "private title"}, None)
	if strings.Contains(string(b), "private title") {
		t.Fatalf("commit title: %s", b)
	}
}

func TestProcsPayloadAgentOnlyScrubbed(t *testing.T) {
	_, b := Encode(Procs, ProcsRow{Processes: []model.Proc{
		{PID: 1, Cmdline: "unrelated private process", Argv: []string{"unrelated", "private", "process"}},
		{PID: 2, Agent: "codex", SessionID: 99, Cmdline: "codex ghp_secretvalue " + strings.Repeat("界", 250)},
	}}, Snippets)
	var got struct {
		Processes []model.Proc `json:"processes"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Processes) != 1 || strings.Contains(string(b), "ghp_") || !strings.Contains(string(b), "[redacted]") || len([]rune(got.Processes[0].Cmdline)) > 200 || strings.Contains(string(b), "session_id") {
		t.Fatalf("procs: %s", b)
	}
}

func TestNaturalReferences(t *testing.T) {
	_, b := Encode(Session, SessionRow{Session: model.Session{ID: 12, ParentID: new(int64), SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "child"}}, Parent: &SessionKey{Adapter: "codex", ExternalID: "parent"}, RepoPath: "/r", WorktreePath: "/w"}, Titles)
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["id"]; ok {
		t.Fatalf("integer ID: %s", b)
	}
	if got["parent"] == nil || got["repo_path"] != "/r" || got["worktree_path"] != "/w" {
		t.Fatalf("references: %s", b)
	}
}

func TestProcsCmdlineDropsPromptsUnlessSnippets(t *testing.T) {
	for _, cmd := range []string{
		`/usr/bin/claude -p "private prompt" --model secret-model --token=private-token`,
		`/opt/bin/codex exec "private task" --config='private config' -q`,
		`claude -p "private --looks-like-flag prompt"`,
		`claude -p"private attached prompt"`,
	} {
		for _, share := range []ShareText{None, Titles, Snippets} {
			for _, capture := range []string{"titles", "snippets"} {
				t.Run(string(share)+"/"+capture+"/"+cmd, func(t *testing.T) {
					argv, err := shellquote.Split(cmd)
					if err != nil {
						t.Fatal(err)
					}
					_, b := Encode(Procs, ProcsRow{Processes: []model.Proc{{Agent: "claude", Cmdline: cmd, Argv: argv}}}, EffectiveShare(share, capture))
					var got struct {
						Processes []model.Proc `json:"processes"`
					}
					if err := json.Unmarshal(b, &got); err != nil {
						t.Fatal(err)
					}
					line := got.Processes[0].Cmdline
					if share == Snippets && capture == "snippets" {
						if line != cmd {
							t.Fatalf("snippet cmdline = %q", line)
						}
						return
					}
					if strings.Contains(line, "private") || strings.Contains(line, "secret") || strings.Contains(line, "looks-like") || strings.Contains(line, "/") || strings.Contains(line, "exec") || strings.Contains(line, "=") {
						t.Fatalf("prompt or argument value exported: %q", line)
					}
					if !strings.Contains(line, "-p") && !strings.Contains(line, "-q") {
						t.Fatalf("flag dropped: %q", line)
					}
				})
			}
		}
	}
}
