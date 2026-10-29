package chat

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestClaudePermissionBridgeArgs(t *testing.T) {
	a, record := fakeCLI(t, flags+"\n --permission-prompts <host> Permission prompts", `for arg in "$@"; do
 if [ "$previous" = "--mcp-config" ]; then /bin/cat "$arg" > "$CHAT_RECORD.config"; fi
 previous="$arg"
done
echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}],"tools":["Read","mcp__driga__kill"]}'
echo '{"type":"result","result":"answer"}'`)
	r := Request{Socket: "/private/chat.sock", Conversation: "conversation", Token: "random-turn-token"}
	if err := a.Run(context.Background(), r, func(Delta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(record + ".args")
	args := string(raw)
	for _, want := range []string{"--tools\nRead,Glob,Grep,TodoWrite\n", "--permission-prompts\nhost\n", "--permission-prompt-tool\nmcp__driga__approve\n", "TodoWrite,mcp__driga__list_prompts,mcp__driga__run_command"} {
		if !strings.Contains(args, want) {
			t.Fatal("missing", want, args)
		}
	}
	if strings.Contains(args, "allowedTools\nmcp__driga__*") || strings.Contains(args, ",Bash") || strings.Contains(args, ",mcp__driga__kill") {
		t.Fatal(args)
	}
	raw, _ = os.ReadFile(record + ".config")
	var conf struct {
		MCPServers map[string]struct {
			Env  map[string]string
			Args []string
		}
	}
	if err := json.Unmarshal(raw, &conf); err != nil {
		t.Fatal(err)
	}
	env := conf.MCPServers["driga"].Env
	if env["DRIGA_CHAT_SOCKET"] != r.Socket || env["DRIGA_CHAT_CONVERSATION"] != r.Conversation || env["DRIGA_CHAT_TOKEN"] != r.Token {
		t.Fatal("per-turn environment missing")
	}
}

// Native command and write tools remain unavailable; commands enter only the broker gateway.
func TestChatRefusesCommandAndWriteTools(t *testing.T) {
	for _, tool := range []string{"Bash", "Edit", "Write"} {
		a, _ := fakeCLI(t, flags+"\n --permission-prompts <host> Permission prompts", `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}],"tools":["Read","`+tool+`"]}'
echo '{"type":"result","result":"answer"}'`)
		r := Request{Socket: "/private/chat.sock", Conversation: "conversation", Token: "token"}
		if err := a.Run(context.Background(), r, func(Delta) error { return nil }); err == nil || !strings.Contains(err.Error(), tool) {
			t.Fatalf("%s: err = %v", tool, err)
		}
	}
}

// A resumed conversation's model may still call Bash, which claude no
// longer offers and refuses; the turn goes on, with no tool row for it.
func TestChatOutlivesACallToAToolNotOffered(t *testing.T) {
	a, _ := fakeCLI(t, flags+"\n --permission-prompts <host> Permission prompts", `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}],"tools":["Read"]}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"du -sk ."}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"No such tool available: Bash"}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"I cannot measure that."}]}}'
echo '{"type":"result","result":"I cannot measure that."}'`)
	var tools []string
	r := Request{Socket: "/private/chat.sock", Conversation: "conversation", Token: "token"}
	if err := a.Run(context.Background(), r, func(d Delta) error {
		if d.Tool != "" {
			tools = append(tools, d.Tool)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tool rows = %v", tools)
	}
}
