package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

// Shaped like `claude --help` 2.1.x: short aliases, commas and argument
// placeholders, so the probe is tested against the real layout.
const flags = `Options:
  -p, --print                           Print response and exit
  --safe-mode                           Start with all customizations off
  --setting-sources <sources>           Comma-separated setting sources
  --strict-mcp-config                   Only use MCP servers from --mcp-config
  --mcp-config <configs...>             Load MCP servers from JSON files
  --tools <tools...>                    Specify the list of available tools
  --allowedTools, --allowed-tools <tools...>
  -n, --name <name>                     Set a display name for the session
  --output-format <format>              Output format (only works with --print)
  --verbose                             Override verbose mode setting
  --include-partial-messages            Include partial message chunks
  -r, --resume [value]                  Resume a conversation`

func fakeCLI(t *testing.T, help, body string) (*Claude, string) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable shell-script fixture")
	}

	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("COPILOT_HOME", t.TempDir())
	t.Setenv("DRIGA_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	t.Setenv("DRIGA_SPOOL_DIR", t.TempDir())
	t.Setenv("CHAT_RECORD", record)
	script := "#!/bin/sh\ncase \"$1\" in --version) echo fake-1;; --help) echo '" + help + "';; *)\nprintf '%s\\n' \"$@\" > \"$CHAT_RECORD.args\"\n/bin/cat > \"$CHAT_RECORD.stdin\"\n" + body + "\n;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return &Claude{DataDir: t.TempDir()}, record
}

func TestCancelledProbeCanRetry(t *testing.T) {
	a, record := fakeCLI(t, flags, `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}'
echo '{"type":"result","result":"answer","is_error":false}'`)
	path := filepath.Join(os.Getenv("PATH"), "claude")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(raw), "--version) echo fake-1", `--version) if [ ! -f "$CHAT_RECORD.retry" ]; then echo ready > "$CHAT_RECORD.probing"; /bin/sleep 60; fi; echo fake-1`, 1)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, Request{}, func(Delta) error { return nil }) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(record + ".probing"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("probe did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("probe was not cancelled")
	}
	if err := os.WriteFile(record+".retry", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background(), Request{}, func(Delta) error { return nil }); err != nil {
		t.Fatalf("cancel poisoned adapter: %v", err)
	}
}

func TestClaudeCancellation(t *testing.T) {
	a, _ := fakeCLI(t, flags, `echo '{"type":"system","subtype":"init","session_id":"cancel-session","mcp_servers":[{"name":"driga","status":"connected"}]}'
/bin/sleep 60`)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, Request{}, func(d Delta) error {
			if d.SessionID != "" {
				close(started)
			}
			return nil
		})
	}()
	// The fake has no vendor or MCP side effects. Stop must bound its child lifetime.
	defer cancel()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("fake exited before init: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("fake did not initialize")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel succeeded without stopping")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("child did not stop")
	}
}
func TestClaudePartialResumeAndTools(t *testing.T) {
	fixture, err := os.ReadFile("testdata/turn.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	a, record := fakeCLI(t, flags, "/bin/cat <<'STREAM'\n"+string(fixture)+"STREAM\n")
	var text, session string
	var tools []string
	var result json.RawMessage
	err = a.Run(context.Background(), Request{Prompt: "question", Resume: "previous", Directory: t.TempDir()}, func(d Delta) error {
		text += d.Text
		if d.SessionID != "" {
			session = d.SessionID
		}
		if d.Tool != "" {
			tools = append(tools, d.Tool)
		}
		if d.Result != nil {
			result = d.Result
		}
		return nil
	})
	if err != nil || text != "Hello world" || session != "fixture-session" || len(tools) != 1 || len(result) == 0 {
		t.Fatal(text, session, tools, string(result), err)
	}
	args, _ := os.ReadFile(record + ".args")
	lines := strings.Split(string(args), "\n")
	for i, arg := range lines {
		if arg == "--mcp-config" {
			if _, err := os.Stat(lines[i+1]); !os.IsNotExist(err) {
				t.Fatal("MCP config leaked", err)
			}
		}
	}
	if !strings.Contains(string(args), "--resume\nprevious\n") || !strings.Contains(string(args), "--tools\n\n") || strings.Contains(string(args), "mcp__driga__*") {
		t.Fatal(string(args))
	}
}
func TestClaudeFailsClosed(t *testing.T) {
	for _, tc := range []struct{ name, help, body, want string }{
		{"flag", strings.ReplaceAll(flags, "--include-partial-messages", ""), "exit 99", "lacks --include-partial-messages"},
		{"mcp", flags, `echo '{"type":"system","subtype":"init","mcp_servers":[]}'`, "driga MCP server"},
		{"shell", flags, `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}],"tools":["Bash"]}'`, "unexpected tool"},
		{"malformed", flags, "echo invalid", "bad claude output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fakeCLI(t, tc.help, tc.body)
			err := a.Run(context.Background(), Request{}, func(Delta) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
		})
	}
}

func TestClaudeTimedPartialsReachBrokerBeforeResult(t *testing.T) {
	a, record := fakeCLI(t, flags, `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}'
echo '{"type":"stream_event","event":{"type":"message_start"}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"First "}}}'
/bin/sleep 0.3
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"second "}}}'
/bin/sleep 0.3
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"First second third"}]}}'
while [ ! -f "$CHAT_RECORD.release" ]; do /bin/sleep 0.01; done
echo '{"type":"result","result":"First second third"}'`)
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := New(context.Background(), st, a, Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	initial, err := b.Read(context.Background(), c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	after := initial.Cursor
	if err := b.Send(context.Background(), c.ID, "question", Context{}); err != nil {
		t.Fatal(err)
	}
	var chunks []string
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for len(chunks) < 3 {
		select {
		case <-deadline:
			t.Fatalf("partials did not arrive before result: %q", chunks)
		case <-tick.C:
			s, err := b.Read(context.Background(), c.ID, after)
			if err != nil {
				t.Fatal(err)
			}
			if s.Reset {
				t.Fatal("unexpected replay reset")
			}
			for _, e := range s.Events {
				if e.Seq != after+1 {
					t.Fatal("unordered replay", after, e)
				}
				after = e.Seq
				if e.Kind == "delta" {
					raw, _ := json.Marshal(e)
					var payload struct {
						Text      string
						MessageID string `json:"message_id"`
					}
					if err := json.Unmarshal(raw, &payload); err != nil {
						t.Fatal(err)
					}
					if payload.MessageID == "" {
						t.Fatal("delta missing message ID")
					}
					chunks = append(chunks, payload.Text)
				}
			}
			if !s.Active || len(s.Result) > 0 {
				t.Fatal("buffered until result", s)
			}
		}
	}
	if got := strings.Join(chunks, ""); got != "First second third" {
		t.Fatal(got)
	}
	if err := os.WriteFile(record+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	s := settled(t, b, c.ID)
	for _, m := range s.Messages {
		if m.Role == "assistant" && m.Text != "First second third" {
			t.Fatal("duplicated or missing assistant text", m.Text)
		}
	}
}

func TestClaudeActionArgsAndFallback(t *testing.T) {
	for _, support := range []bool{true, false} {
		t.Run(map[bool]string{true: "actions", false: "fallback"}[support], func(t *testing.T) {
			help := flags
			if support {
				help += "\n --permission-prompts <host> Host prompts"
			}
			body := `previous=""
for arg in "$@"; do
 if [ "$previous" = "--mcp-config" ]; then /bin/cp "$arg" "$CHAT_RECORD.config"; fi
 previous="$arg"
done
echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}'
echo '{"type":"result","result":"ok"}'`
			a, record := fakeCLI(t, help, body)
			notices := 0
			r := Request{Socket: "private.sock", Conversation: "conv", Token: "random-token"}
			emit := func(d Delta) error {
				if d.Notice != "" {
					notices++
				}
				return nil
			}
			for i := 0; i < 2; i++ {
				if err := a.Run(context.Background(), r, emit); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := os.ReadFile(record + ".args")
			args := string(raw)
			if support {
				for _, want := range []string{"--tools\ndefault\n", "--permission-prompts\nhost\n", "--permission-prompt-tool\nmcp__driga__approve\n", "TodoWrite,mcp__driga__list_prompts"} {
					if !strings.Contains(args, want) {
						t.Fatal(want, args)
					}
				}
				if strings.Contains(args, "--allowedTools\nmcp__driga__*") || notices != 0 {
					t.Fatal(args, notices)
				}
			} else if !strings.Contains(args, "--tools\n\n") || strings.Contains(args, "--permission-prompt") || notices != 2 {
				t.Fatal(args, notices)
			}
			raw, _ = os.ReadFile(record + ".config")
			var conf struct {
				Servers map[string]struct {
					Args []string
					Env  map[string]string
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(raw, &conf); err != nil {
				t.Fatal(err)
			}
			env := conf.Servers["driga"].Env
			if support && (env["DRIGA_CHAT_TOKEN"] != r.Token || env["DRIGA_CHAT_SOCKET"] != r.Socket || env["DRIGA_CHAT_CONVERSATION"] != r.Conversation) {
				t.Fatal("turn environment missing")
			}
			if !support && (env["DRIGA_CHAT_TOKEN"] != "" || env["DRIGA_CHAT_READ_ONLY"] != "1" || strings.Contains(args, "mcp__driga__list_prompts")) {
				t.Fatal("fallback enabled actions")
			}
			r.ReadOnly = true
			if err := a.Run(context.Background(), r, emit); err != nil {
				t.Fatal(err)
			}
			raw, _ = os.ReadFile(record + ".args")
			if strings.Contains(string(raw), "--permission-prompt") || strings.Contains(string(raw), "mcp__driga__list_prompts") || !strings.Contains(string(raw), "--tools\n\n") {
				t.Fatal("ask enabled actions")
			}
		})
	}
}

// Text from a second model message, after a tool call, starts a new
// paragraph instead of running on from the first: "gate.I didn't".
func TestClaudeSeparatesConsecutiveMessages(t *testing.T) {
	a, _ := fakeCLI(t, flags, `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}'
echo '{"type":"stream_event","event":{"type":"message_start"}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Sending it through the gate."}}}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Sending it through the gate."}]}}'
echo '{"type":"stream_event","event":{"type":"message_start"}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"It was declined."}}}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"It was declined."}]}}'
echo '{"type":"result","result":"It was declined."}'`)
	var text strings.Builder
	if err := a.Run(context.Background(), Request{}, func(d Delta) error { text.WriteString(d.Text); return nil }); err != nil {
		t.Fatal(err)
	}
	if got := text.String(); got != "Sending it through the gate.\n\nIt was declined." {
		t.Fatalf("text = %q", got)
	}
}

// A dashboard write's card waits for its result: a save names the
// dashboard and version it saved, a rejected draft says why.
func TestPanelCardsCarryWhatTheWriteDid(t *testing.T) {
	help := flags + "\n  --permission-prompts <mode>  Host prompts\n  --permission-prompt-tool <tool>  Prompt tool"
	a, _ := fakeCLI(t, help, `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__driga__create_dashboard","input":{"dashboard":{"name":"Memory","panels":[]}}},{"type":"tool_use","id":"t2","name":"mcp__driga__edit_dashboard","input":{"id":"home","operations":[]}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"{\"dashboard\":{\"name\":\"Memory\",\"panels\":[]},\"id\":\"memory-0e6be9\",\"version\":1,\"results\":[]}"},{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"invalid_spec: panels[0].source: unknown source"}]}}'
echo '{"type":"result","result":"done"}'`)
	var cards []card
	r := Request{Socket: "private.sock", Conversation: "conv", Token: "random-token"}
	if err := a.Run(context.Background(), r, func(d Delta) error {
		if len(d.Panel) > 0 {
			var c card
			if err := json.Unmarshal(d.Panel, &c); err != nil {
				return err
			}
			cards = append(cards, c)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("cards = %+v", cards)
	}
	byTool := map[string]card{cards[0].Tool: cards[0], cards[1].Tool: cards[1]}
	if c := byTool["create_dashboard"]; c.Saved == nil || c.Saved.ID != "memory-0e6be9" || c.Saved.Version != 1 || c.Error != "" {
		t.Fatalf("create card = %+v", c)
	}
	if c := byTool["edit_dashboard"]; c.Saved != nil || !strings.Contains(c.Error, "unknown source") {
		t.Fatalf("edit card = %+v", c)
	}
}
