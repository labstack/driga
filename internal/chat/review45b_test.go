package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestClaudeReadsUsePermissionBridge(t *testing.T) {
	a, record := fakeCLI(t, flags+"\n --permission-prompts <host> Host prompts", `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}],"tools":["Read","Glob","Grep","LS","NotebookRead","mcp__driga__list_prompts"]}'
echo '{"type":"result","result":"ok"}'`)
	r := Request{Socket: "private.sock", Conversation: "conv", Token: "token"}
	if err := a.Run(context.Background(), r, func(Delta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(record + ".args")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(raw), "\n")
	allowed := ""
	for i, arg := range args {
		if arg == "--allowedTools" && i+1 < len(args) {
			allowed = args[i+1]
		}
	}
	tools := strings.Split(allowed, ",")
	for _, tool := range tools {
		switch tool {
		case "Read", "Glob", "Grep", "LS", "NotebookRead", "Bash":
			t.Fatalf("pre-approved %s", tool)
		}
	}
	if !strings.Contains(allowed, "TodoWrite") || !strings.Contains(allowed, "mcp__driga__list_prompts") {
		t.Fatal(allowed)
	}
}

func TestOutsideReadsAskByMode(t *testing.T) {
	for _, mode := range []Mode{Confirm, Auto, FullAuto} {
		for _, tool := range []string{"Read", "Bash"} {
			t.Run(string(mode)+"/"+tool, func(t *testing.T) {
				b, r := actionTurn(t)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := b.SetMode(ctx, r.Conversation, mode); err != nil {
					t.Fatal(err)
				}
				in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: tool, Input: json.RawMessage(`{"file_path":"/etc/hosts","command":"head /etc/hosts"}`)}
				done := make(chan Permission, 1)
				go func() { p, _ := b.Approve(ctx, in); done <- p }()
				if mode != FullAuto {
					card := waitAction(t, b, r.Conversation)
					if err := b.Decide(ctx, r.Conversation, card.ID, false); err != nil {
						t.Fatal(err)
					}
				}
				if p := <-done; (p.Behavior == "allow") != (mode == FullAuto) {
					t.Fatal(p)
				}
			})
		}
	}
}

func TestRemoteSessionActionsRefused(t *testing.T) {
	for _, tool := range []string{"kill", "jump"} {
		for _, mode := range []Mode{Confirm, Auto, FullAuto} {
			t.Run(tool+"/"+string(mode), func(t *testing.T) {
				b, r := actionTurn(t)
				// The context store can be separate from the ephemeral chat history.
				state, err := store.Open("", store.Memory)
				if err != nil {
					t.Fatal(err)
				}
				defer state.Close()
				b.opts.ContextStore = state
				var local, remote int64
				if err := state.Tx(context.Background(), func(tx *store.Tx) error {
					var err error
					local, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "local"}})
					if err != nil {
						return err
					}
					remote, err = tx.ForMachine("peer").UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote"}})
					return err
				}); err != nil {
					t.Fatal(err)
				}
				in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__" + tool, Input: json.RawMessage(fmt.Sprintf(`{"session_id":%d,"machine":"local"}`, remote))}
				if err := b.SetMode(context.Background(), r.Conversation, mode); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				p, err := b.Approve(ctx, in)
				if err != nil || p.Behavior != "deny" || !strings.Contains(p.Message, "another machine") {
					t.Fatal(p, err)
				}
				in.Input = json.RawMessage(fmt.Sprintf(`{"session_id":%d}`, local))
				if b.remoteTarget(in) {
					t.Fatal("local session refused")
				}
			})
		}
	}
}

func TestStoppedActionCompletesWithOwnToken(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			b, a, _ := fixture(t, true)
			ctx := context.Background()
			in := activeApproval(t, b, a, Context{})
			if err := b.SetMode(ctx, in.Conversation, FullAuto); err != nil {
				t.Fatal(err)
			}
			if p, err := b.Approve(ctx, in); err != nil || p.Behavior != "allow" {
				t.Fatal(p, err)
			}
			id, err := b.Claim(in)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.Stop(ctx, in.Conversation); err != nil {
				t.Fatal(err)
			}
			s := settled(t, b, in.Conversation)
			if len(s.Actions) != 1 || s.Actions[0].Status != "stopped" || s.Actions[0].Result != "result unknown" {
				t.Fatal(s.Actions)
			}
			if _, err := b.Claim(in); err == nil {
				t.Fatal("stopped token claimed new work")
			}
			// Other conversations must not evict a claimed action awaiting its result.
			for range MaxConversations {
				row, err := b.Create(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := b.Read(ctx, row.ID, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := b.Send(ctx, in.Conversation, "next turn", Context{}); err != nil {
				t.Fatal(err)
			}
			next := <-a.starts
			wrong := in
			wrong.Token = next.Token
			if err := b.Complete(wrong, id, "wrong", false); err == nil {
				t.Fatal("new turn completed old action")
			}
			if err := b.Complete(in, id, "actual result", failed); err != nil {
				t.Fatal(err)
			}
			if err := b.Complete(in, id, "replay", false); err == nil {
				t.Fatal("completion replayed")
			}
			s, err = b.Read(ctx, in.Conversation, 0)
			if err != nil {
				t.Fatal(err)
			}
			want := "done"
			if failed {
				want = "failed"
			}
			if s.Actions[0].Status != want || s.Actions[0].Result != "actual result" {
				t.Fatal(s.Actions)
			}
			rows, err := b.st.ChatMessages(in.Conversation)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range rows {
				if m.ID == id && (m.Status != want || !strings.Contains(m.Text, "actual result")) {
					t.Fatal(m)
				}
			}
		})
	}
}

type deadlineTransport func(*http.Request) (*http.Response, error)

func (f deadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRemoteRequestTimeouts(t *testing.T) {
	b, _, _ := fixture(t, true)
	dir := t.TempDir()
	stop, err := Listen(context.Background(), dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c, err := dialPath(context.Background(), SocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, path := range []string{"/conversations", "/send", "/stop", "/state", "/mode", "/approve", "/hook-prompt"} {
		t.Run(path, func(t *testing.T) {
			want := 10 * time.Second
			if path == "/approve" || path == "/hook-prompt" {
				want = ApprovalTTL + time.Minute
			}
			c.http.Transport = deadlineTransport(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining > want || remaining < want-time.Second {
					t.Errorf("deadline %v; want %v", remaining, want)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			})
			if err := c.call(context.Background(), "POST", path, nil, nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c.http.Transport = deadlineTransport(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > time.Second {
					t.Error("lost caller deadline")
				}
				return nil, context.Canceled
			})
			if err := c.call(ctx, "POST", path, nil, nil); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestPromptCancellationAndAnswerAtomic(t *testing.T) {
	for _, answerFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(answerFirst), func(t *testing.T) {
			b, _, _ := fixture(t, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan PromptDecision, 1)
			go func() { p, _ := b.WaitPrompt(ctx, testPrompt("race")); done <- p }()
			waitPrompt(t, b, "race")
			b.mu.Lock()
			if !answerFirst {
				cancel()
			}
			err := b.answerPrompt("race", true)
			cancel()
			b.mu.Unlock()
			p := <-done
			if answerFirst {
				if err != nil || p.Behavior != "allow" {
					t.Fatal(p, err)
				}
			} else if err == nil || err.Error() != "prompt no longer waiting" || p.Behavior != "" {
				t.Fatal(p, err)
			}
		})
	}
}

func TestRetiredPromptActionCard(t *testing.T) {
	b, r := actionTurn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { b.WaitPrompt(ctx, testPrompt("gone")); close(done) }()
	waitPrompt(t, b, "gone")
	if err := b.SetMode(context.Background(), r.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__answer_prompt", Input: json.RawMessage(`{"id":"gone","allow":true}`)}
	if p, err := b.Approve(context.Background(), in); err != nil || p.Behavior != "allow" {
		t.Fatal(p, err)
	}
	id, err := b.Claim(in)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	if err := b.AnswerApprovedPrompt(in, id); err == nil || err.Error() != "prompt no longer waiting" {
		t.Fatal(err)
	}
	s, err := b.Read(context.Background(), r.Conversation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Actions[0].Status != "failed" || s.Actions[0].Result != "prompt no longer waiting" {
		t.Fatal(s.Actions)
	}
}

func TestReadOnlyNoticePerConversation(t *testing.T) {
	cli, _ := fakeCLI(t, flags, `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}'
echo '{"type":"result","result":"ok"}'`)
	b, _, _ := fixture(t, true)
	b.agent = cli
	for range 2 {
		row, err := b.Create(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for turn := range 3 {
			if turn == 2 {
				b.mu.Lock()
				b.dispose(b.conversations[row.ID])
				delete(b.conversations, row.ID)
				b.mu.Unlock()
			}
			if err := b.Send(context.Background(), row.ID, "question", Context{}); err != nil {
				t.Fatal(err)
			}
			s := settled(t, b, row.ID)
			count := 0
			for _, m := range s.Messages {
				if m.Role == "system" && (m.Status == "notice" || strings.HasPrefix(m.Text, "Chat stays read-only:")) {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("conversation %s turn %d: %d notices", row.ID, turn, count)
			}
		}
	}
}

func TestAutoProjectOncePerTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell fixture")
	}
	bin, record := t.TempDir(), filepath.Join(t.TempDir(), "git-calls")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf 'probe\\n' >> \"$CHAT_GIT_RECORD\"\necho true\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("CHAT_GIT_RECORD", record)
	b, a, _ := fixture(t, true)
	b.opts.Ephemeral = true
	row, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for turn := range 2 {
		project := t.TempDir()
		if err := b.Send(context.Background(), row.ID, "edit", Context{Project: project}); err != nil {
			t.Fatal(err)
		}
		r := <-a.starts
		if err := b.SetMode(context.Background(), row.ID, Auto); err != nil {
			t.Fatal(err)
		}
		for _, tool := range []string{"Edit", "Write", "Edit"} {
			in := Approval{Conversation: row.ID, Token: r.Token, Tool: tool, Input: json.RawMessage(`{"file_path":"ordinary.txt"}`)}
			if p, err := b.Approve(context.Background(), in); err != nil || p.Behavior != "allow" {
				t.Fatal(p, err)
			}
		}
		raw, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		if calls := strings.Count(string(raw), "probe\n"); calls != turn+1 {
			t.Fatalf("turn %d: %d git probes", turn, calls)
		}
		a.release <- struct{}{}
		settled(t, b, row.ID)
	}
}
