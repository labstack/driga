package chat

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func actionTurn(t *testing.T) (*Broker, Request) {
	t.Helper()
	b, a, _ := fixture(t, true)
	closeSocket, err := Listen(context.Background(), t.TempDir(), b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeSocket)
	c, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Send(context.Background(), c.ID, "act", Context{}); err != nil {
		t.Fatal(err)
	}
	return b, <-a.starts
}
func waitAction(t *testing.T, b *Broker, id string) Action {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, err := b.Read(context.Background(), id, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range s.Actions {
			if a.Status == "pending" {
				return a
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no pending action")
	return Action{}
}
func TestApprovalBridgeDecisions(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{true: "allow", false: "deny"}[allow], func(t *testing.T) {
			b, r := actionTurn(t)
			ctx := context.Background()
			in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "Bash", Input: json.RawMessage(`{"command":"echo hello > file"}`), ToolUseID: "call"}
			done := make(chan Permission, 1)
			errs := make(chan error, 1)
			go func() { p, e := Bridge(ctx, r.Socket, in); done <- p; errs <- e }()
			a := waitAction(t, b, r.Conversation)
			other, _ := b.Create(ctx)
			if err := b.Decide(ctx, other.ID, a.ID, true); err == nil {
				t.Fatal("wrong conversation decided")
			}
			if err := b.Decide(ctx, r.Conversation, a.ID, allow); err != nil {
				t.Fatal(err)
			}
			p := <-done
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
			var updated map[string]string
			_ = json.Unmarshal(p.UpdatedInput, &updated)
			if (p.Behavior == "allow") != allow || allow && updated["command"] != "echo hello > file" {
				t.Fatal(p)
			}
			if err := b.Decide(ctx, r.Conversation, a.ID, true); err == nil {
				t.Fatal("decided twice")
			}
			if allow {
				b.mu.Lock()
				b.finishAction(b.conversations[r.Conversation], "call", "done", "ok")
				b.mu.Unlock()
			}
			rows, err := b.st.ChatMessages(r.Conversation)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range rows {
				if m.Role == "action" {
					found = true
					if strings.HasPrefix(m.Text, "{") {
						t.Fatal("JSON in stored action")
					}
				}
			}
			if !found {
				t.Fatal("missing action log")
			}
		})
	}
}
func TestApprovalStopExpiryAndCapabilities(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		t.Run(map[bool]string{true: "expiry", false: "stop"}[expiry], func(t *testing.T) {
			b, r := actionTurn(t)
			if expiry {
				b.approvalTTL = 15 * time.Millisecond
			}
			in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "Bash", Input: json.RawMessage(`{}`)}
			bad := in
			bad.Token = "wrong"
			if _, err := Bridge(context.Background(), r.Socket, bad); err == nil {
				t.Fatal("wrong token")
			}
			bad = in
			bad.Conversation = "wrong"
			if _, err := Bridge(context.Background(), r.Socket, bad); err == nil {
				t.Fatal("wrong conversation")
			}
			done := make(chan Permission, 1)
			go func() { p, _ := Bridge(context.Background(), r.Socket, in); done <- p }()
			a := waitAction(t, b, r.Conversation)
			if !expiry {
				if err := b.Stop(context.Background(), r.Conversation); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case p := <-done:
				if p.Behavior != "deny" {
					t.Fatal(p)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("permission stuck")
			}
			s, _ := b.Read(context.Background(), r.Conversation, 0)
			want := "denied"
			if expiry {
				want = "expired"
			}
			for _, got := range s.Actions {
				if got.ID == a.ID && got.Status != want {
					t.Fatal(got)
				}
			}
		})
	}
	b, r := actionTurn(t)
	for _, path := range []string{"/mode", "/decide"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"id":"`+r.Conversation+`","mode":"full auto"}`))
		req.Header.Set("Authorization", r.Token)
		w := httptest.NewRecorder()
		handler(b).ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	raw := `{"conversation":"` + r.Conversation + `","token":"` + r.Token + `","tool_name":"Bash","input":{},"mode":"full auto"}`
	w := httptest.NewRecorder()
	handler(b).ServeHTTP(w, httptest.NewRequest("POST", "/approve", strings.NewReader(raw)))
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	s, _ := b.Read(context.Background(), r.Conversation, 0)
	if s.Mode != Confirm {
		t.Fatal(s.Mode)
	}
}
func TestTrustModesAndClaims(t *testing.T) {
	b, r := actionTurn(t)
	ctx := context.Background()
	if err := b.SetMode(ctx, r.Conversation, Auto); err != nil {
		t.Fatal(err)
	}
	in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__ack_inbox", Input: json.RawMessage(`{"id":"item"}`)}
	if _, err := Claim(ctx, r.Socket, in); err == nil {
		t.Fatal("unapproved tool claimed")
	}
	p, err := Bridge(ctx, r.Socket, in)
	if err != nil || p.Behavior != "allow" {
		t.Fatal(p, err)
	}
	wrong := in
	wrong.Input = json.RawMessage(`{"id":"other"}`)
	if _, err := Claim(ctx, r.Socket, wrong); err == nil {
		t.Fatal("different input claimed")
	}
	id, err := Claim(ctx, r.Socket, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Claim(ctx, r.Socket, in); err == nil {
		t.Fatal("replayed claim")
	}
	if err := Complete(ctx, r.Socket, in, id, "acknowledged", false); err != nil {
		t.Fatal(err)
	}
	if err := b.SetMode(ctx, r.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	in.Tool = "Unknown"
	p, err = Bridge(ctx, r.Socket, in)
	if err != nil || p.Behavior != "allow" {
		t.Fatal(p, err)
	}
	b.mu.Lock()
	b.conversations[r.Conversation].remote = true
	b.mu.Unlock()
	p, err = Bridge(ctx, r.Socket, in)
	if err != nil || p.Behavior != "deny" || !strings.Contains(p.Message, "another machine") {
		t.Fatal(p, err)
	}
}
func TestAutoRules(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, ".git", "hooks"), filepath.Join(dir, "hooks")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tool, path string
		want       bool
	}{
		{"Edit", "file", true}, {"Write", filepath.Join(dir, "new", "file"), true},
		{"Write", "../escape", false}, {"Write", "~/outside", false}, {"Write", filepath.Join(outside, "file"), false},
		{"Write", "escape/file", false}, {"Write", "escape/../file", false}, {"Write", "escape/../file", false}, {"Write", "dangling", false},
		// Files that run code or steer agents without a command always ask.
		{"Write", ".git/hooks/pre-commit", false}, {"Edit", ".claude/settings.json", false}, {"Write", ".mcp.json", false},
		{"Write", ".envrc", false}, {"Write", ".vscode/tasks.json", false}, {"Edit", "CLAUDE.md", false}, {"Edit", "sub/AGENTS.md", false},
		{"Write", ".github/workflows/ci.yml", false}, {"Write", "hooks/pre-commit", false}, {"Write", ".GIT/config", false},
		{"Edit", "src/claude.go", true}, {"Write", "docs/git.md", true},
		{"Bash", "file", false}, {"WebFetch", "file", false}, {"Unknown", "file", false},
		{"mcp__driga__kill", "", false}, {"mcp__driga__cleanup", "", false},
		{"mcp__driga__ack_inbox", "", true}, {"mcp__driga__jump", "", true}, {"mcp__driga__summarize", "", true},
	} {
		raw, _ := json.Marshal(map[string]string{"file_path": tc.path})
		if got := autoAllowed(tc.tool, raw, dir, outside); got != tc.want {
			t.Errorf("%s %s: %v", tc.tool, tc.path, got)
		}
	}
	if autoAllowed("Write", json.RawMessage(`{}`), dir, outside) {
		t.Fatal("missing path allowed")
	}
}

func TestClaimInputKeepsLargeIDs(t *testing.T) {
	a, err := canonicalInput(json.RawMessage(`{"session_id":9007199254740992}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalInput(json.RawMessage(`{"session_id":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("rounded distinct session IDs")
	}
}
