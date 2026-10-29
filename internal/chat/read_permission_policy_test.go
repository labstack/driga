package chat

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClaudeReadsUsePermissionBridge(t *testing.T) {
	a, record := fakeCLI(t, flags+"\n --permission-prompts <host> Host prompts", `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}],"tools":["ToolSearch","Read","Glob","Grep","TodoWrite","mcp__driga__list_prompts"]}'
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
				{
					card := waitAction(t, b, r.Conversation)
					if err := b.Decide(ctx, r.Conversation, card.ID, false); err != nil {
						t.Fatal(err)
					}
				}
				if p := <-done; p.Behavior != "deny" {
					t.Fatal(p)
				}
			})
		}
	}
}
