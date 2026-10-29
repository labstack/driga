package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/command"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestChatActionNeedsPermission(t *testing.T) {
	readEnv(t)
	t.Setenv("DRIGA_CHAT_SOCKET", "")
	t.Setenv("DRIGA_CHAT_TOKEN", "")
	t.Setenv("DRIGA_CHAT_CONVERSATION", "")
	_, err := chatAction(&Globals{})(context.Background(), "ack_inbox", json.RawMessage(`{"id":"one"}`))
	if err == nil {
		t.Fatal("action ran without permission bridge")
	}
	if _, err := os.Stat(config.SpoolDir()); !os.IsNotExist(err) {
		t.Fatal("unapproved action wrote spool")
	}
}
func TestChatAckUsesCLI(t *testing.T) {
	readEnv(t)
	var out bytes.Buffer
	if err := runChatAction(context.Background(), &Globals{Machine: "local", Stdout: &out}, "ack_inbox", json.RawMessage(`{"id":"one"}`)); err != nil {
		t.Fatal(err)
	}
	events, _, err := spool.Drain(config.SpoolDir(), nil)
	if err != nil || len(events) != 1 || events[0].Message != "one" || events[0].Event != "ack" {
		t.Fatal(events, err)
	}
	if err := runChatAction(context.Background(), &Globals{Stdout: &out}, "ack_inbox", json.RawMessage(`{"id":"one","all":true}`)); err == nil {
		t.Fatal("invalid selection bypassed CLI check")
	}
}
func TestChatKillKeepsVerification(t *testing.T) {
	readEnv(t)
	start := time.Now().Add(-time.Hour)
	tree := func(cpu float64) []model.Proc {
		return []model.Proc{{PID: 4242, PPID: 1, StartTime: start, CPUSeconds: cpu}}
	}
	readDB(t, func(tx *store.Tx) error {
		return tx.ReplaceOrphans([]model.Orphan{{ID: "busy", RootPID: 4242, StartTime: start, PIDs: []int32{4242}}})
	})
	plat := &cliSeqPlatform{snaps: [][]model.Proc{tree(1), tree(2)}}
	oldPlat, oldWindow := actPlatform, verifyWindow
	actPlatform = func() platform.Platform { return plat }
	verifyWindow = time.Millisecond
	t.Cleanup(func() { actPlatform, verifyWindow = oldPlat, oldWindow })
	var out bytes.Buffer
	err := runChatAction(context.Background(), &Globals{Machine: "local", Stdout: &out}, "kill", json.RawMessage(`{"id":"busy","force":true}`))
	if err == nil || !strings.Contains(err.Error(), "not idle") {
		t.Fatal(err, out.String())
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatal("second approval prompt")
	}
}
func TestChatSessionKillKeepsIdentity(t *testing.T) {
	readEnv(t)
	var id int64
	readDB(t, func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "test", PID: 4242, ProcStart: time.Now().Add(-time.Hour)}})
		return err
	})
	var out bytes.Buffer
	err := runChatAction(context.Background(), &Globals{Stdout: &out}, "kill", json.RawMessage(fmt.Sprintf(`{"session_id":%d}`, id)))
	if err == nil || !strings.Contains(err.Error(), "changed or exited") {
		t.Fatal(err)
	}
}
func TestChatJumpUsesStoredDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stored destination is a tmux pane; Windows jumps go to Windows Terminal")
	}
	readEnv(t)
	t.Setenv("TMUX", "/tmp/fake,1,0")
	var id int64
	readDB(t, func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "test"}})
		if err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "item", SessionID: id, Kind: "waiting", TS: time.Now()}); err != nil {
			return err
		}
		return tx.SetInboxJump("item", &model.InboxJump{Kind: "tmux", Server: "/tmp/fake", Target: "dev:1.2"})
	})
	old := newInboxRunner
	t.Cleanup(func() { newInboxRunner = old })
	calls := 0
	newInboxRunner = func() attention.Runner {
		return jumpRunner(func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls++
			if name != "tmux" || strings.Contains(strings.Join(args, " "), "send-keys") {
				t.Fatalf("unexpected jump %s %q", name, args)
			}
			return nil, nil
		})
	}
	var out bytes.Buffer
	err := runChatAction(context.Background(), &Globals{Stdout: &out}, "jump", json.RawMessage(fmt.Sprintf(`{"session_id":%d}`, id)))
	if err != nil || calls == 0 {
		t.Fatal(err, calls)
	}
}
func TestChatCleanupAndSummaryKeepChecks(t *testing.T) {
	readEnv(t)
	var out bytes.Buffer
	g := &Globals{Machine: "local", Stdout: &out, CleanupPlatform: &cliSeqPlatform{snaps: [][]model.Proc{{}}}}
	for _, tc := range []struct{ tool, raw, want string }{{"cleanup", `{"id":"missing"}`, "unknown"}, {"summarize", `{"project":"api"}`, "summaries are off"}} {
		err := runChatAction(context.Background(), g, tc.tool, json.RawMessage(tc.raw))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatal(tc.tool, err)
		}
	}
}

func TestChatListPromptsOnlyWithActions(t *testing.T) {
	for _, policy := range []string{"", "titles", "snippets"} {
		for _, actions := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", policy, actions), func(t *testing.T) {
				ctx := context.Background()
				a, b := sdk.NewInMemoryTransports()
				ss, err := newMCPServer(&Globals{}, true, actions, policy).Connect(ctx, a, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer ss.Close()
				cs, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(ctx, b, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer cs.Close()
				listing, err := cs.ListTools(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, tool := range listing.Tools {
					if tool.Name == "list_prompts" {
						found = true
					}
				}
				commandFound := false
				for _, tool := range listing.Tools {
					if tool.Name == "run_command" {
						commandFound = true
					}
				}
				if commandFound != actions {
					t.Fatalf("actions=%t: run_command=%t", actions, commandFound)
				}
				if found != actions {
					t.Fatalf("actions=%t: advertised list_prompts=%t", actions, found)
				}
			})
		}
	}
}

func TestAgentsKillAndChatShareIdentityAndIdleChecks(t *testing.T) {
	for _, scenario := range []string{"identity changed", "busy"} {
		t.Run(scenario, func(t *testing.T) {
			readEnv(t)
			start := time.Now().Add(-time.Hour)
			var id int64
			procs := []model.Proc{{PID: 4242, PPID: 1, StartTime: start, CPUSeconds: 1}}
			readDB(t, func(tx *store.Tx) error {
				var err error
				id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "selected", PID: 4242, ProcStart: start}})
				if err != nil {
					return err
				}
				if scenario == "identity changed" {
					return nil
				}
				return tx.ReplaceProcs(procs)
			})
			oldPlat, oldWindow := actPlatform, verifyWindow
			defer func() { actPlatform, verifyWindow = oldPlat, oldWindow }()
			verifyWindow = time.Millisecond
			for _, caller := range []string{"CLI", "Chat"} {
				later := append([]model.Proc(nil), procs...)
				later[0].CPUSeconds = 2
				actPlatform = func() platform.Platform { return &cliSeqPlatform{snaps: [][]model.Proc{procs, later}} }
				var out, diagnostic bytes.Buffer
				var err error
				if caller == "CLI" {
					code := run([]string{"agents", "kill", fmt.Sprint(id), "--force", "-y"}, "test", nil, &out, &diagnostic)
					if code != 1 {
						t.Fatal(code, &out, &diagnostic)
					}
					err = fmt.Errorf("%s", diagnostic.String())
				} else {
					err = runChatAction(context.Background(), &Globals{Stdout: &out}, "kill", json.RawMessage(fmt.Sprintf(`{"session_id":%d,"force":true}`, id)))
				}
				want := "changed or exited"
				if scenario == "busy" {
					want = "not idle"
				}
				if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(out.String(), "signaled") {
					t.Fatal(caller, err, &out)
				}
			}
		})
	}
}

func TestRunCommandBrokerDispatch(t *testing.T) {
	readEnv(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "broker.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if runtime.GOOS != "windows" {
		if err = os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := make(chan chat.Approval, 2)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/run-command" {
			http.Error(w, "wrong route", 400)
			return
		}
		var in chat.Approval
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		calls <- in
		_ = json.NewEncoder(w).Encode(command.Result{Output: "broker output"})
	})}
	go server.Serve(listener)
	defer server.Close()
	t.Setenv("DRIGA_CHAT_SOCKET", path)
	t.Setenv("DRIGA_CHAT_CONVERSATION", "conversation")
	t.Setenv("DRIGA_CHAT_TOKEN", "turn-token")
	marker := filepath.Join(dir, "must-not-exist")
	raw, _ := json.Marshal(map[string]any{"script": "echo executed > " + marker, "cwd": dir})
	result, err := chatAction(&Globals{})(context.Background(), "run_command", raw)
	if err != nil || !strings.Contains(result, "broker output") {
		t.Fatal(result, err)
	}
	in := <-calls
	if in.Tool != "mcp__driga__run_command" || in.Token != "turn-token" || in.Conversation != "conversation" || in.ToolUseID == "" || string(in.Input) != string(raw) {
		t.Fatal(in)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("MCP child executed command")
	}
}
