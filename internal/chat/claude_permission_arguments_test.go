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
echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}],"tools":["Bash","Read","mcp__driga__kill"]}'
echo '{"type":"result","result":"answer"}'`)
	r := Request{Socket: "/private/chat.sock", Conversation: "conversation", Token: "random-turn-token"}
	if err := a.Run(context.Background(), r, func(Delta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(record + ".args")
	args := string(raw)
	for _, want := range []string{"--tools\ndefault\n", "--permission-prompts\nhost\n", "--permission-prompt-tool\nmcp__driga__approve\n", "TodoWrite,mcp__driga__list_prompts"} {
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
