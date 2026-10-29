package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/command"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func commandTurn(t *testing.T) (*Broker, Approval, string) {
	t.Helper()
	b, a, _ := fixture(t, true)
	in := activeApproval(t, b, a, Context{})
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "read.txt"), []byte("HEAD hello TAIL\n"), 0600); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	c := b.conversations[in.Conversation]
	c.directory = project
	c.commandProject = project
	b.mu.Unlock()
	in.Tool = "mcp__driga__run_command"
	in.ToolUseID = "command-1"
	raw, _ := json.Marshal(command.Request{Argv: []string{"cat", "read.txt"}})
	in.Input = raw
	return b, in, project
}
func TestCommandBrokerLifecycle(t *testing.T) {
	b, in, _ := commandTurn(t)
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { _, err := b.RunCommand(ctx, in); done <- err }()
	card := waitAction(t, b, in.Conversation)
	if card.Command == nil || card.Command.Class != command.Read || card.Command.Requested.TimeoutSeconds != 120 {
		t.Fatal("missing prepared card", card)
	}
	var output bytes.Buffer
	if err := b.st.ReadCommandOutput(ctx, card.ID, &output); err != nil || output.Len() != 0 {
		t.Fatal("execution preceded decision", err)
	}
	if _, err := b.Claim(in); err == nil {
		t.Fatal("child claimed command")
	}
	if err := b.Decide(ctx, in.Conversation, card.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := b.st.ReadCommandOutput(ctx, card.ID, &output); err != nil || !strings.Contains(output.String(), "HEAD hello TAIL") {
		t.Fatal(output.String(), err)
	}
	state, _ := b.Read(ctx, in.Conversation, 0)
	if state.Actions[0].Status != "done" {
		t.Fatal(state.Actions)
	}
	in.ToolUseID = "never"
	in.Input = json.RawMessage(`{"argv":["sudo","true"]}`)
	if _, err := b.RunCommand(ctx, in); err == nil {
		t.Fatal("Never executed")
	}
	state, _ = b.Read(ctx, in.Conversation, 0)
	if len(state.Actions) != 1 {
		t.Fatal("Never offered a card")
	}
	t.Run("record-failure", func(t *testing.T) {
		b, in, _ := commandTurn(t)
		b.st.Close()
		if _, err := b.RunCommand(ctx, in); err == nil {
			t.Fatal("unretained action started")
		}
	})
}
func TestCommandTurnLifecycle(t *testing.T) {
	for _, kind := range []string{"ask", "remote", "wrong-token", "stop", "timeout", "new-turn"} {
		t.Run(kind, func(t *testing.T) {
			b, in, _ := commandTurn(t)
			ctx := context.Background()
			switch kind {
			case "ask":
				b.conversations[in.Conversation].readOnly = true
			case "remote":
				b.conversations[in.Conversation].remote = true
			case "wrong-token":
				in.Token = "old"
			case "new-turn":
				b.conversations[in.Conversation].token = "new"
			}
			if kind == "ask" || kind == "remote" || kind == "wrong-token" || kind == "new-turn" {
				if _, err := b.RunCommand(ctx, in); err == nil {
					t.Fatal("invalid turn executed")
				}
				return
			}
			b.approvalTTL = 100 * time.Millisecond
			done := make(chan error, 1)
			go func() { _, err := b.RunCommand(ctx, in); done <- err }()
			card := waitAction(t, b, in.Conversation)
			if kind == "stop" {
				if err := b.Stop(ctx, in.Conversation); err != nil {
					t.Fatal(err)
				}
			}
			if err := <-done; err == nil {
				t.Fatal("retired request executed")
			}
			if err := b.Decide(ctx, in.Conversation, card.ID, true); err == nil {
				t.Fatal("retired card accepted")
			}
		})
	}
}
func TestCommandForceSecondCard(t *testing.T) {
	b, in, _ := commandTurn(t)
	in.Input = json.RawMessage(`{"argv":["kill","-9","12345"]}`)
	if _, err := b.RunCommand(context.Background(), in); err == nil {
		t.Fatal("initial force accepted")
	}
}

// Synthetic identities are never signalled: execution sees only the broker.
type commandPlatform struct {
	platform.Platform
	rows []model.Proc
}

func (p *commandPlatform) Procs(context.Context) ([]model.Proc, error) { return p.rows, nil }
func commandProcessFixture(t *testing.T) (*Broker, Approval, command.Prepared, model.Proc) {
	t.Helper()
	b, in, _ := commandTurn(t)
	self := model.Proc{PID: int32(os.Getpid()), PPID: 1, StartTime: time.Now(), OwnerKnown: true, OwnerUID: uint32(os.Getuid()), Exe: "broker", Cmdline: "broker", Argv: []string{"broker"}}
	target := model.Proc{PID: 2000000000, PPID: 1, StartTime: time.Now(), OwnerKnown: true, OwnerUID: uint32(os.Getuid()), Exe: "dummy", Cmdline: "dummy unique", Argv: []string{"dummy", "unique"}}
	plat := &commandPlatform{rows: []model.Proc{self, target}}
	b.opts.CommandPlatform = plat
	in.Input = json.RawMessage(`{"argv":["pkill","-f","dummy unique"]}`)
	p, err := b.prepareCommand(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return b, in, p, target
}
func TestCommandForceCardBoundToSurvivors(t *testing.T) {
	b, in, p, target := commandProcessFixture(t)
	ctx := context.Background()
	force, err := command.PrepareForce(p, []model.Proc{target})
	if err != nil {
		t.Fatal(err)
	}
	if command.Digest(p) == command.Digest(force) {
		t.Fatal("force reused TERM digest")
	}
	done := make(chan error, 1)
	go func() { _, err := b.approveCommand(ctx, in, p); done <- err }()
	card := waitAction(t, b, in.Conversation)
	if card.Command.Force || len(card.Command.Targets) != 1 {
		t.Fatal(card)
	}
	if err = b.Decide(ctx, in.Conversation, card.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	// No signal is sent: the fake live table now reports that target as vanished.
	plat := b.opts.CommandPlatform.(*commandPlatform)
	plat.rows = plat.rows[:1]
	result, err := b.executeCommand(ctx, in, card.ID, command.Digest(p))
	if err != nil || len(result.Processes.Vanished) != 1 {
		t.Fatal(result, err)
	}
	in.ToolUseID = "second-force"
	go func() { _, err := b.approveCommand(ctx, in, force); done <- err }()
	second := waitAction(t, b, in.Conversation)
	if second.ID == card.ID || !second.Command.Force || len(second.Command.Targets) != 1 {
		t.Fatal(second)
	}
	if err = b.Decide(ctx, in.Conversation, second.ID, false); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err == nil {
		t.Fatal("force denial ignored")
	}
	if _, err = b.executeCommand(ctx, in, second.ID, command.Digest(force)); err == nil {
		t.Fatal("denied force executed")
	}
}
func TestCommandBrokerStopRunning(t *testing.T) {
	b, in, project := commandTurn(t)
	ctx := context.Background()
	cap := command.ProbeSandbox(ctx)
	if !cap.Available {
		t.Skip("native execution unavailable: " + cap.Reason)
	}
	marker := filepath.Join(project, "started")
	in.Input = json.RawMessage(`{"script":"echo ready > started; while :; do sleep 1; done","timeout_seconds":30}`)
	done := make(chan error, 1)
	go func() { _, err := b.RunCommand(ctx, in); done <- err }()
	card := waitAction(t, b, in.Conversation)
	if err := b.Decide(ctx, in.Conversation, card.ID, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := b.Stop(ctx, in.Conversation); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Stop did not cancel execution")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command survived Stop")
	}
}

func TestCommandBrokerAutomaticRead(t *testing.T) {
	b, in, _ := commandTurn(t)
	ctx := context.Background()
	if err := b.SetMode(ctx, in.Conversation, Auto); err != nil {
		t.Fatal(err)
	}
	result, err := b.RunCommand(ctx, in)
	if err != nil || !strings.Contains(result.Output, "HEAD hello TAIL") {
		t.Fatal(result, err)
	}
	state, err := b.Read(ctx, in.Conversation, 0)
	if err != nil || len(state.Actions) != 1 || state.Actions[0].Status != "done" || state.Actions[0].DecidedBy != "auto" {
		t.Fatal(state.Actions, err)
	}
	if _, err = b.RunCommand(ctx, in); err == nil {
		t.Fatal("duplicate gateway request executed")
	}
}
func TestCommandBrokerProtectsLiveDescendants(t *testing.T) {
	b, in, _, _ := commandProcessFixture(t)
	plat := b.opts.CommandPlatform.(*commandPlatform)
	plat.rows[1].PPID = int32(os.Getpid())
	if _, err := b.prepareCommand(context.Background(), in); err == nil {
		t.Fatal("Chat startup descendant selected")
	}
	plat.rows = plat.rows[1:]
	if _, err := b.prepareCommand(context.Background(), in); err == nil {
		t.Fatal("unverified broker root accepted")
	}
}
