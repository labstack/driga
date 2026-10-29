package chat

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/command"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func grantFirst(t *testing.T) (*Broker, Approval, command.Prepared, string) {
	t.Helper()
	b, in, _ := commandTurn(t)
	p, e := b.prepareCommand(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := b.approveCommand(context.Background(), in, p); done <- e }()
	card := waitAction(t, b, in.Conversation)
	if !card.GrantEligible || card.GrantPattern == "" {
		_, why := command.NewArgvPattern(p)
		t.Fatal("missing grant offer", card, "pattern", why, "sandbox", p.Sandbox)
	}
	if e = b.DecideSimilar(context.Background(), in.Conversation, card.ID); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if e = b.DecideSimilar(context.Background(), in.Conversation, card.ID); e == nil {
		t.Fatal("replayed grant creation")
	}
	return b, in, p, card.ID
}
func TestGrantAuditAndRevocation(t *testing.T) {
	b, in, p, _ := grantFirst(t)
	ctx := context.Background()
	in.ToolUseID = "next"
	id, e := b.approveCommand(ctx, in, p)
	if e != nil {
		t.Fatal(e)
	}
	s, _ := b.Read(ctx, in.Conversation, 0)
	a := s.Actions[len(s.Actions)-1]
	if a.ID != id || a.DecidedBy != "allowed by your grant" || a.GrantID == "" {
		t.Fatal(a)
	}
	if e = b.RevokeGrant(ctx, a.GrantID); e != nil {
		t.Fatal(e)
	}
	if _, e = b.executeCommand(ctx, in, id, command.Digest(p)); e == nil {
		t.Fatal("revoked grant consumed")
	}
	history, e := b.GrantHistory(ctx)
	if e != nil || len(history) != 1 || history[0].Attribution != "allowed by your grant" {
		t.Fatal(history, e)
	}
	raw, _ := json.Marshal(history)
	if strings.Contains(string(raw), "read.txt") {
		t.Fatal("history includes inputs", string(raw))
	}
}
func TestGrantScopeAndExpiry(t *testing.T) {
	for _, kind := range []string{"expiry", "eviction", "scope"} {
		t.Run(kind, func(t *testing.T) {
			b, in, p, _ := grantFirst(t)
			ctx := context.Background()
			now := time.Now()
			b.mu.Lock()
			b.opts.Now = func() time.Time { return now.Add(30 * time.Minute) }
			b.mu.Unlock()
			if kind == "eviction" {
				b.mu.Lock()
				b.dispose(b.conversations[in.Conversation])
				b.mu.Unlock()
			}
			if kind == "scope" {
				b.mu.Lock()
				b.opts.Now = func() time.Time { return now }
				b.mu.Unlock()
				in.Conversation = "another"
			}
			in.ToolUseID = "later"
			cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
			defer cancel()
			if _, e := b.approveCommand(cctx, in, p); e == nil {
				t.Fatal("expired scope granted")
			}
		})
	}
}
func TestGrantRequiresAppClick(t *testing.T) {
	b, in, _, _ := grantFirst(t)
	for _, path := range []string{"/decide", "/prompt-answer", "/grant-revoke", "/grant-history"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"id":"x","similar":true}`))
		r.Header.Set("Authorization", in.Token)
		w := httptest.NewRecorder()
		handler(b).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	for _, raw := range []string{`{"id":"p","allow":true,"similar":true}`, `{"id":"p","allow":true,"grant":"x"}`} {
		if _, _, e := ParsePromptAnswer([]byte(raw)); e == nil {
			t.Fatal("model grant field accepted")
		}
	}
	for _, class := range []command.Class{command.Process, command.Destructive, command.Never} {
		if _, e := command.NewArgvPattern(command.Prepared{Policy: command.Policy{Class: class}}); e == nil {
			t.Fatal(class)
		}
	}
}

func TestGrantSessionLifecycle(t *testing.T) {
	b, _, root := commandTurn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := func(id, cwd, tool, input string) <-chan PromptDecision {
		done := make(chan PromptDecision, 1)
		go func() {
			v, _ := b.WaitPrompt(ctx, Prompt{ID: id, Session: "hook-session", Cwd: cwd, Tool: tool, Input: json.RawMessage(input)})
			done <- v
		}()
		return done
	}
	if err := b.st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "hook-session", Cwd: root, StartedAt: time.Unix(100, 0)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	readTool, readInput := "Bash", `{"command":"cat read.txt"}`
	if runtime.GOOS == "windows" {
		readTool, readInput = "run_command", `{"argv":["type","read.txt"]}`
	}
	first := start("first", root, readTool, readInput)
	waitPrompt(t, b, "first")
	ps, _ := b.Prompts(ctx)
	if !ps[0].GrantEligible {
		t.Fatal("missing session offer", ps)
	}
	if err := b.AnswerPromptSimilar(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if (<-first).Behavior != "allow" {
		t.Fatal("first denied")
	}
	next := start("next", root, readTool, readInput)
	select {
	case d := <-next:
		if d.Behavior != "allow" {
			t.Fatal(d)
		}
	case <-time.After(time.Second):
		t.Fatal("grant did not answer")
	}
	history, _ := b.GrantHistory(ctx)
	if len(history) != 1 || history[0].Session != "hook-session" {
		t.Fatal(history)
	}
	// A reused external/database session ID with a new process/session lifetime
	// cannot inherit the old grant, even before an end-hook is observed.
	if err := b.st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "hook-session", Cwd: root, StartedAt: time.Unix(200, 0)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reused := start("reused-lifetime", root, readTool, readInput)
	waitPrompt(t, b, "reused-lifetime")
	_ = b.AnswerPrompt(ctx, "reused-lifetime", false)
	<-reused
	b.EndPrompts("hook-session", root)
	third := start("third", root, readTool, readInput)
	waitPrompt(t, b, "third")
	if err := b.AnswerPrompt(ctx, "third", false); err != nil {
		t.Fatal(err)
	}
	if (<-third).Behavior != "deny" {
		t.Fatal("reused session inherited grant")
	}
	unsupported := start("write", root, "Write", `{"file_path":"read.txt","content":"new"}`)
	waitPrompt(t, b, "write")
	ps, _ = b.Prompts(ctx)
	if ps[0].GrantEligible {
		t.Fatal("unsupported tool offered")
	}
	_ = b.AnswerPrompt(ctx, "write", false)
	<-unsupported
}
func TestDelegatedAllowAlwaysNeedsPersonWithGrant(t *testing.T) {
	b, in, _, _ := grantFirst(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = b.WaitPrompt(ctx, testPrompt("delegated-grant")) }()
	waitPrompt(t, b, "delegated-grant")
	in.Tool = "mcp__driga__answer_prompt"
	in.ToolUseID = "delegated"
	in.Input = json.RawMessage(`{"id":"delegated-grant","allow":true}`)
	done := make(chan Permission, 1)
	go func() { v, _ := b.Approve(ctx, in); done <- v }()
	card := waitAction(t, b, in.Conversation)
	if card.Delegated == nil || card.GrantEligible || card.GrantID != "" {
		t.Fatal(card)
	}
	select {
	case <-done:
		t.Fatal("delegated answer auto-allowed")
	default:
	}
	_ = b.Decide(ctx, in.Conversation, card.ID, false)
	<-done
}

func TestGrantCommandStillPreparedAndRecorded(t *testing.T) {
	b, in, _, _ := grantFirst(t)
	in.ToolUseID = "recorded"
	result, err := b.RunCommand(context.Background(), in)
	if err != nil || !strings.Contains(result.Output, "HEAD hello TAIL") {
		t.Fatal(result, err)
	}
	state, _ := b.Read(context.Background(), in.Conversation, 0)
	a := state.Actions[len(state.Actions)-1]
	if a.Command == nil || a.Status != "done" || a.DecidedBy != grantAttribution {
		t.Fatal(a)
	}
}

func TestGrantSessionUnknownNoOffer(t *testing.T) {
	b, _, root := commandTurn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = b.WaitPrompt(ctx, Prompt{ID: "unknown-session", Session: "unknown", Cwd: root, Tool: "Bash", Input: json.RawMessage(`{"command":"cat read.txt"}`)})
	}()
	waitPrompt(t, b, "unknown-session")
	ps, _ := b.Prompts(ctx)
	if ps[0].GrantEligible {
		t.Fatal("grant offered without a collected session lifetime")
	}
}

func TestGitReadHasNoExternalGrant(t *testing.T) {
	b, in, root := commandTurn(t)
	ctx := context.Background()
	for _, tc := range []struct{ tool, input string }{
		{"Bash", `{"command":"git status"}`},
		{"run_command", `{"argv":["git","status"]}`},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			p := Prompt{SessionID: 1, Cwd: root, Tool: tc.tool, Input: json.RawMessage(tc.input)}
			if got := b.preparePromptGrant(ctx, p); got != nil {
				t.Fatalf("external grant would run unhardened git: %#v", got.Policy)
			}
		})
	}
	in.Input = json.RawMessage(`{"argv":["git","status"]}`)
	prepared, err := b.prepareCommand(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// The Windows broker has no trusted Git read backend. It must refuse this
		// sandboxed command instead of granting it automatic execution.
		if prepared.Policy.Class != command.Destructive || commandAutomatic(Auto, prepared) {
			t.Fatal(prepared.Policy)
		}
		_, err := b.RunCommand(ctx, in)
		const want = "command denied: Running commands needs a sandbox, which Windows does not have yet."
		if err == nil || err.Error() != want {
			t.Fatal(err)
		}
		return
	}
	if prepared.Policy.Class != command.Read || !commandAutomatic(Auto, prepared) {
		t.Fatalf("broker-hardened Git read lost automatic authorization: %#v", prepared.Policy)
	}
}

func TestExternalGrantsOnlyAllowValidatedReadAllowlist(t *testing.T) {
	b, _, root := commandTurn(t)
	for _, tc := range []struct {
		argv []string
		want bool
	}{
		{[]string{"type", "read.txt"}, false},
		{[]string{"ls", "-la", "."}, true},
		{[]string{"cat", "read.txt"}, true},
		{[]string{"head", "-n", "1", "read.txt"}, true},
		{[]string{"tail", "-n", "1", "read.txt"}, true},
		{[]string{"wc", "-l", "read.txt"}, true},
		{[]string{"stat", "read.txt"}, true},
		{[]string{"du", "-sh", "."}, true},
		{[]string{"df", "-h"}, true},
		{[]string{"rg", "hello", "read.txt"}, false},
		{[]string{"grep", "hello", "read.txt"}, false},
		{[]string{"git", "status"}, false},
		{[]string{"find", ".", "-type", "f"}, false},
		{[]string{"ps", "-ef"}, false},
		{[]string{"lsof", "-n"}, false},
		{[]string{"which", "cat"}, false},
		{[]string{"pgrep", "-f", "pattern"}, false},
		{[]string{"uptime"}, false},
		// Other commands can get broker-sandboxed grants, but never external ones.
		{[]string{"echo", "hello"}, false},
		{[]string{"ls", "--unknown"}, false},
		{[]string{"cat", "--unknown"}, false},
		{[]string{"tail", "-f", "read.txt"}, false},
		{[]string{"./cat", "read.txt"}, false},
	} {
		for _, tool := range []string{"Bash", "run_command"} {
			t.Run(tool+"/"+strings.Join(tc.argv, " "), func(t *testing.T) {
				var input []byte
				if tool == "Bash" {
					input, _ = json.Marshal(map[string]string{"command": strings.Join(tc.argv, " ")})
				} else {
					input, _ = json.Marshal(command.Request{Argv: tc.argv})
				}
				want := tc.want
				if runtime.GOOS == "windows" {
					want = tool == "run_command" && tc.argv[0] == "type"
				}
				p := Prompt{SessionID: 1, Cwd: root, Tool: tool, Input: input}
				if got := b.preparePromptGrant(context.Background(), p); (got != nil) != want {
					t.Fatalf("external grant eligible = %t, want %t", got != nil, want)
				}
			})
		}
	}
}
