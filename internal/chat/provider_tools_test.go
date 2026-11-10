package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestProviderToolChild(t *testing.T) {
	mode := os.Getenv("DRIGA_TEST_TOOL_CHILD")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fixture", "version": "1"}}
		case "tools/list":
			names := []string{"get_summary", "list_agents", "list_prompts", "run_command", "create_dashboard", "edit_dashboard", "approve", "delete_dashboard", "replace_dashboard", "restore_dashboard_version", "kill", "cleanup", "unknown"}
			if mode == "duplicate" {
				names = append(names, "get_summary")
			}
			catalog := []map[string]any{}
			for _, name := range names {
				catalog = append(catalog, map[string]any{"name": name, "description": "fixture " + name, "inputSchema": map[string]any{"type": "object"}})
			}
			result = map[string]any{"tools": catalog}
			if mode == "pages" {
				result = map[string]any{"tools": []any{}, "nextCursor": "repeat"}
			}
		case "tools/call":
			if marker := os.Getenv("DRIGA_TEST_TOOL_MARKER"); marker != "" {
				f, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					os.Exit(2)
				}
				fmt.Fprintln(f, string(request.Params))
				f.Close()
			}
			if mode == "block" {
				time.Sleep(time.Hour)
				os.Exit(2)
			}
			if mode == "oversize" {
				result = map[string]any{"content": []map[string]string{{"type": "text", "text": strings.Repeat("x", maxLine+1)}}}
				break
			}
			if mode == "input" {
				result = json.RawMessage(`{"resultType":"input_required","inputRequests":{},"content":[]}`)
				break
			}
			if mode == "error" {
				result = map[string]any{"content": []map[string]string{{"type": "text", "text": "refused by broker"}}, "isError": true}
				break
			}
			result = json.RawMessage(`{"content":[{"type":"text","text":"fixture result"}],"structuredContent":{"id":9007199254740993}}`)
		default:
			result = map[string]any{}
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func providerToolFixture(t *testing.T, ctx context.Context, actions bool, mode string) (*providerTools, string, error) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "calls")
	p, err := startProviderTools(ctx, actions, func(childCtx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestProviderToolChild$")
		cmd.Env = append(os.Environ(), "DRIGA_TEST_TOOL_CHILD="+mode, "DRIGA_TEST_TOOL_MARKER="+marker)
		return cmd
	})
	if err == nil {
		t.Cleanup(func() { _ = p.Close() })
	}
	return p, marker, err
}

func TestProviderToolCatalogRestrictsAuthority(t *testing.T) {
	for _, actions := range []bool{false, true} {
		t.Run(fmt.Sprint(actions), func(t *testing.T) {
			p, marker, err := providerToolFixture(t, context.Background(), actions, "normal")
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, tool := range p.Catalog() {
				names = append(names, tool.Name)
			}
			want := []string{"get_summary", "list_agents"}
			if actions {
				want = append(want, "list_prompts", "run_command", "create_dashboard", "edit_dashboard")
			}
			if !slices.Equal(names, want) {
				t.Fatal(names)
			}
			catalog := p.Catalog()
			catalog[0].InputSchema[0] = '!'
			if p.Catalog()[0].InputSchema[0] != '{' {
				t.Fatal("catalog aliases internal schema")
			}
			denied := []string{"approve", "delete_dashboard", "replace_dashboard", "restore_dashboard_version", "kill", "cleanup", "unknown", "mcp__driga__get_summary"}
			if !actions {
				denied = append(denied, "run_command", "list_prompts", "create_dashboard", "edit_dashboard")
			}
			for _, name := range denied {
				if _, err := p.Call(context.Background(), name, json.RawMessage(`{}`)); err == nil {
					t.Fatal("unsafe tool accepted", name)
				}
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("rejected tools reached MCP child", err)
			}
			result, err := p.Call(context.Background(), "get_summary", json.RawMessage(`{"id":9007199254740993}`))
			if err != nil || result.Failed || !strings.Contains(result.Text, "9007199254740993") {
				t.Fatal(result, err)
			}
			b, err := os.ReadFile(marker)
			if err != nil || !strings.Contains(string(b), "9007199254740993") {
				t.Fatal("argument integer rounded", string(b), err)
			}
		})
	}
}

func TestProviderToolArgumentAndResultLimits(t *testing.T) {
	p, marker, err := providerToolFixture(t, context.Background(), true, "normal")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{`[]`, `null`, `{`, `{} {}`, `{"x":"` + strings.Repeat("x", MaxText) + `"}`} {
		if _, err := p.Call(context.Background(), "get_summary", json.RawMessage(args)); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("invalid arguments dispatched", err)
	}
	large, _, err := providerToolFixture(t, context.Background(), true, "oversize")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := large.Call(context.Background(), "get_summary", nil); err == nil {
		t.Fatal("oversized result accepted")
	}
	failed, _, err := providerToolFixture(t, context.Background(), true, "error")
	if err != nil {
		t.Fatal(err)
	}
	result, err := failed.Call(context.Background(), "run_command", json.RawMessage(`{}`))
	if err != nil || !result.Failed || !strings.Contains(result.Text, "refused by broker") {
		t.Fatal(result, err)
	}
}

func TestProviderToolCatalogRejectsDuplicateAndLoop(t *testing.T) {
	for _, mode := range []string{"duplicate", "pages"} {
		if p, _, err := providerToolFixture(t, context.Background(), true, mode); err == nil {
			p.Close()
			t.Fatal("bad catalog accepted", mode)
		}
	}
}

func TestProviderToolCancelReapsChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, marker, err := providerToolFixture(t, ctx, true, "block")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := p.Call(context.Background(), "run_command", json.RawMessage(`{}`)); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("tool not dispatched")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled call succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled child did not unblock call")
	}
	_ = p.Close()
	_ = p.Close()
	if _, err := p.Call(context.Background(), "get_summary", nil); err == nil {
		t.Fatal("closed turn accepted tool")
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "\n") != 1 {
		t.Fatal("cancelled tool replayed", string(b), err)
	}
}

func TestProviderToolEnvironmentRemovesInheritedAuthority(t *testing.T) {
	got := providerToolEnvironment([]string{"HOME=/fixture", "DRIGA_CHAT_TOKEN=old", "driga_chat_socket=old", "DRIGA_CHAT_READ_ONLY=0", "DRIGA_INTERNAL=old", "DRIGA_DATA_DIR=old"}, map[string]string{"DRIGA_DATA_DIR": "/data", "DRIGA_CHAT_READ_ONLY": "1"})
	want := []string{"HOME=/fixture", "DRIGA_DATA_DIR=/data", "DRIGA_CHAT_READ_ONLY=1", "DRIGA_INTERNAL=1"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	if _, err := openProviderTools(context.Background(), t.TempDir(), os.Args[0], Request{}); err == nil {
		t.Fatal("missing bridge accepted")
	}
}

func TestProviderToolDoesNotReplayInputRequest(t *testing.T) {
	p, marker, err := providerToolFixture(t, context.Background(), true, "input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(context.Background(), "run_command", nil); err == nil || !strings.Contains(err.Error(), "not replayed") {
		t.Fatal(err)
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "\n") != 1 {
		t.Fatal("tool automatically replayed", string(b), err)
	}
}

func TestProviderToolTurnBudget(t *testing.T) {
	p, marker, err := providerToolFixture(t, context.Background(), true, "normal")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if _, err := p.Call(context.Background(), "get_summary", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Call(context.Background(), "get_summary", nil); err == nil {
		t.Fatal("turn budget ignored")
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "\n") != 128 {
		t.Fatal("extra request dispatched", err)
	}
}
