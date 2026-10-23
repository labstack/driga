package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func gitProject(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func TestAutoRequiresSafeProjectDirectory(t *testing.T) {
	root := t.TempDir()
	home, data := filepath.Join(root, "home"), filepath.Join(root, "state", "data")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, path := range []string{home, data, filepath.Join(root, "state"), filepath.Join(home, "project", "src")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	gitProject(t, root)
	for _, dir := range []string{root, home, data, filepath.Dir(data)} {
		if autoAllowed("Write", json.RawMessage(`{"file_path":"ordinary.txt"}`), dir, autoProject(dir, data)) {
			t.Errorf("broad working directory allowed: %s", dir)
		}
	}
	plain := t.TempDir()
	if autoAllowed("Edit", json.RawMessage(`{"file_path":"ordinary.txt"}`), plain, autoProject(plain, data)) {
		t.Fatal("non-project directory allowed")
	}
	// Git discovery must ignore inherited overrides, and accept project subdirs.
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", plain)
	if autoAllowed("Write", json.RawMessage(`{"file_path":"ordinary.txt"}`), plain, autoProject(plain, data)) {
		t.Fatal("Git environment made a non-project directory a project")
	}
	for _, dir := range []string{filepath.Join(home, "project"), filepath.Join(home, "project", "src")} {
		if !autoAllowed("Write", json.RawMessage(`{"file_path":"ordinary.txt"}`), dir, autoProject(dir, data)) {
			t.Errorf("project working directory refused: %s", dir)
		}
	}
}

func TestAutoHiddenAndInstructionComponents(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	gitProject(t, dir)
	for _, path := range []string{
		"CLAUDE.local.md", "sub/AGENTS.override.md", "Gemini-team.MD", "sub/COPILOT-instructions.md",
		".cursorrules", ".windsurfrules", ".cursor/tasks.json", ".zed/settings.json", "sub/.new-agent/config",
		"CLAUDE.local.md/file", "sub/agents-extra.md/file", ".mcp.json", ".envrc",
	} {
		for _, tool := range []string{"Edit", "Write"} {
			raw, _ := json.Marshal(map[string]string{"file_path": path})
			if autoAllowed(tool, raw, dir, autoProject(dir, data)) {
				t.Errorf("%s allowed %s", tool, path)
			}
		}
	}
	protected := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(protected, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(protected, filepath.Join(dir, "ordinary.md")); err != nil {
		t.Fatal(err)
	}
	if autoAllowed("Edit", json.RawMessage(`{"file_path":"ordinary.md"}`), dir, autoProject(dir, data)) {
		t.Fatal("resolved instruction file allowed through symlink")
	}
}

func TestAutoWindowsNormalizedComponents(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	gitProject(t, dir)
	for _, path := range []string{
		`.git.\hooks\pre-commit`, `.git \config`, `src\CLAUDE.local.md. `,
		`CLAUDE.md::$DATA`, `file:stream`, `src/ordinary.txt:stream`, `sub/AGENTS.override.md. `,
		`src/.cursor. /settings`, `src/Copilot-Instructions.md `,
	} {
		raw, _ := json.Marshal(map[string]string{"file_path": path})
		if autoAllowed("Write", raw, dir, autoProject(dir, data)) {
			t.Errorf("Windows alias allowed: %q", path)
		}
	}
}

func TestReloadRetiresPendingActions(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	row, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a late failed shutdown save, including a legacy row without Tool.
	for _, tool := range []string{"Bash", ""} {
		m := store.ChatMessage{ID: "pending-" + tool, ConversationID: row.ID, Role: "action", Tool: tool, Text: "Kill target · pending", Status: "pending", CreatedAt: time.Now()}
		if err := st.SaveChatMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	// Keep a failing version from hanging fixture cleanup on its nil channel.
	t.Cleanup(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, c := range b.conversations {
			for _, a := range c.actions {
				if a.answer == nil {
					a.Status = "denied"
				}
			}
		}
	})
	s, err := b.Read(ctx, row.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range s.Messages {
		if m.Status != "denied" || strings.Contains(m.Text, "pending") || !strings.Contains(m.Text, "interrupted") {
			t.Fatal("stale restored card", m)
		}
	}
	if len(s.Actions) != 1 || s.Actions[0].Status != "denied" {
		t.Fatal(s.Actions)
	}
	if err := b.Decide(ctx, row.ID, "pending-Bash", true); err == nil {
		t.Fatal("restored action was actionable")
	}
	stored, err := st.ChatMessages(row.ID)
	if err != nil || len(stored) != 2 || stored[0].Status != "denied" || stored[1].Status != "denied" {
		t.Fatal(stored, err)
	}
}

func TestResolveWithoutAnswerChannel(t *testing.T) {
	b, _, _ := fixture(t, true)
	row, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := &conversation{row: row}
	a := &pendingAction{Action: Action{ID: "orphaned", Tool: "Bash", Status: "pending"}}
	done := make(chan error, 1)
	go func() { done <- b.resolve(c, a, true, "you", "") }()
	select {
	case err := <-done:
		if err != nil || a.Status != "denied" {
			t.Fatal(a, err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolve blocked on absent waiter")
	}
}

func TestPromptApprovalTitle(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			b, r := actionTurn(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go b.WaitPrompt(ctx, testPrompt("target"))
			waitPrompt(t, b, "target")
			in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__answer_prompt", Input: json.RawMessage(fmt.Sprintf(`{"id":"target","allow":%t}`, allow))}
			done := make(chan Permission, 1)
			go func() { p, _ := b.Approve(ctx, in); done <- p }()
			card := waitAction(t, b, r.Conversation)
			decision := "Deny"
			if allow {
				decision = "Allow"
			}
			for _, text := range []string{decision, "claude session", "/repo", "Bash", "private prompt input"} {
				if !strings.Contains(card.Title, text) {
					t.Fatalf("title %q missing %q", card.Title, text)
				}
			}
			if err := b.Decide(ctx, r.Conversation, card.ID, false); err != nil {
				t.Fatal(err)
			}
			<-done
			rows, err := b.st.ChatMessages(r.Conversation)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range rows {
				if m.Role == "action" && (!strings.Contains(m.Text, "answer_prompt target") || strings.Contains(m.Text, "private prompt input")) {
					t.Fatal("prompt audit lost its identity or retained input", m)
				}
			}
		})
	}
}

func TestApprovalLookupDoesNotHoldBrokerLock(t *testing.T) {
	for _, tool := range []string{"mcp__driga__jump", "mcp__driga__ack_inbox", "mcp__driga__kill"} {
		t.Run(tool, func(t *testing.T) {
			b, r := actionTurn(t)
			// Block only the collected-state connection, leaving chat history writable.
			state, err := store.Open("", store.Memory)
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			b.opts.ContextStore = state
			locked, release, released := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				released <- state.Tx(context.Background(), func(*store.Tx) error { close(locked); <-release; return nil })
			}()
			<-locked
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				b.Approve(ctx, Approval{Conversation: r.Conversation, Token: r.Token, Tool: tool, Input: json.RawMessage(`{"id":"target","session_id":1}`)})
			}()
			// Give the approval goroutine time to reach the blocked store lookup.
			time.Sleep(30 * time.Millisecond)
			read := make(chan error, 1)
			go func() { _, err := b.Read(ctx, r.Conversation, 0); read <- err }()
			var readErr error
			select {
			case readErr = <-read:
			case <-time.After(300 * time.Millisecond):
				readErr = fmt.Errorf("approval store lookup held broker lock")
			}
			close(release)
			if err := <-released; err != nil {
				t.Fatal(err)
			}
			cancel()
			<-done
			if readErr != nil {
				t.Fatal(readErr)
			}
		})
	}
}

func TestPromptsListingDoesNotQueryStore(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.WaitPrompt(ctx, testPrompt("one"))
	waitPrompt(t, b, "one")
	locked, release, released := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { released <- st.Tx(ctx, func(*store.Tx) error { close(locked); <-release; return nil }) }()
	<-locked
	read := make(chan error, 1)
	go func() {
		ps, err := b.Prompts(ctx)
		if err == nil && len(ps) != 1 {
			err = fmt.Errorf("missing prompt: %v", ps)
		}
		read <- err
	}()
	var err error
	select {
	case err = <-read:
	case <-time.After(300 * time.Millisecond):
		err = fmt.Errorf("prompt listing waited for store")
	}
	close(release)
	<-released
	if err != nil {
		t.Fatal(err)
	}
}

func TestPromptSessionChecksAreThrottled(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	done := make(chan PromptDecision, 2)
	for _, id := range []string{"one", "two"} {
		go func() { p, _ := b.WaitPrompt(ctx, testPrompt(id)); done <- p }()
		waitPrompt(t, b, id)
	}
	now := time.Now()
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "session", Cwd: "/repo"}, EndedAt: &now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("session checks ran before the shared five-second tick")
	case <-time.After(1200 * time.Millisecond):
	}
	// The shared ticker must retire both, while the SessionEnd hook stays immediate.
	for range 2 {
		select {
		case p := <-done:
			if p.Behavior != "" {
				t.Fatal(p)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("shared session check did not retire prompt")
		}
	}
}

func TestActionInputsOnlyOnResetOrActionEvent(t *testing.T) {
	b, r := actionTurn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan Permission, 1)
	input, _ := json.Marshal(map[string]string{"file_path": "file", "content": strings.Repeat("large-input", 8000)})
	go func() {
		p, _ := b.Approve(ctx, Approval{Conversation: r.Conversation, Token: r.Token, Tool: "Write", Input: input})
		done <- p
	}()
	card := waitAction(t, b, r.Conversation)
	snapshot, err := b.Read(ctx, r.Conversation, 0)
	if err != nil || !snapshot.Reset || len(snapshot.Actions) != 1 || string(snapshot.Actions[0].Input) != string(input) {
		t.Fatal("missing full reset", err)
	}
	b.mu.Lock()
	c := b.conversations[r.Conversation]
	b.appendEvent(c, Event{Kind: "delta", MessageID: "reply", Text: "next token"})
	b.mu.Unlock()
	delta, err := b.Wait(ctx, r.Conversation, snapshot.Cursor)
	if err != nil || delta.Reset || len(delta.Actions) != 0 || len(delta.Events) != 1 || delta.Events[0].Kind != "delta" {
		t.Fatal("delta resent action state", err)
	}
	if err := b.Decide(ctx, r.Conversation, card.ID, false); err != nil {
		t.Fatal(err)
	}
	<-done
	update, err := b.Read(ctx, r.Conversation, delta.Cursor)
	if err != nil || update.Reset || len(update.Actions) != 0 || len(update.Events) != 1 || update.Events[0].Action == nil || update.Events[0].Action.Status != "denied" {
		t.Fatal("action event missing", update, err)
	}
	replay, err := b.Read(ctx, r.Conversation, update.Cursor+1)
	if err != nil || !replay.Reset || len(replay.Actions) != 1 || replay.Actions[0].Status != "denied" {
		t.Fatal("reset did not restore updated card", err)
	}
}
