// Fake Claude: probe/stream-json, and the real built driga mcp --chat over stdio.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
)

type step struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	// Barrier is a fixture-only HTTP rendezvous, outside the command gateway.
	Barrier string `json:"barrier,omitempty"`
	Replay  bool   `json:"replay,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	a := os.Args[1:]
	if slices.Equal(a, []string{"--version"}) {
		fmt.Println("2.9.0 (Claude Code)")
		return nil
	}
	if slices.Equal(a, []string{"--help"}) {
		for _, f := range strings.Fields("print mcp-config strict-mcp-config setting-sources tools allowedTools output-format verbose include-partial-messages permission-prompts permission-prompt-tool name resume") {
			fmt.Print("--" + f + " ")
		}
		fmt.Println()
		return nil
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string
			Args    []string
			Env     map[string]string
		} `json:"mcpServers"`
	}
	for i, arg := range a {
		if arg == "--mcp-config" && i+1 < len(a) {
			b, e := os.ReadFile(a[i+1])
			if e != nil {
				return e
			}
			if e = json.Unmarshal(b, &cfg); e != nil {
				return e
			}
		}
	}
	server := cfg.MCPServers["driga"]
	if len(server.Args) < 2 || server.Args[0] != "mcp" || !slices.Contains(server.Args, "--chat") {
		return fmt.Errorf("expected built driga mcp --chat")
	}
	cmd := exec.Command(server.Command, server.Args...)
	cmd.Env = os.Environ()
	for k, v := range server.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = os.Stderr
	in, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	defer func() { in.Close(); cmd.Process.Kill(); cmd.Wait() }()
	r := bufio.NewReader(out)
	id := 0
	rpc := func(method string, params any) (json.RawMessage, error) {
		id++
		if e := json.NewEncoder(in).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); e != nil {
			return nil, e
		}
		for {
			line, e := r.ReadBytes('\n')
			if e != nil {
				return nil, e
			}
			var response struct {
				ID     int
				Result json.RawMessage
				Error  json.RawMessage
			}
			if e = json.Unmarshal(line, &response); e != nil {
				return nil, e
			}
			if response.ID != id {
				continue
			}
			if len(response.Error) > 0 {
				// A real agent consumes tool refusals and still finishes its turn.
				b, _ := json.Marshal(map[string]any{"isError": true, "error": response.Error})
				return b, nil
			}
			return response.Result, nil
		}
	}
	if _, e = rpc("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "chat-command-fixture", "version": "1"}}); e != nil {
		return e
	}
	if e = json.NewEncoder(in).Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); e != nil {
		return e
	}
	list, e := rpc("tools/list", map[string]any{})
	if e != nil {
		return e
	}
	var listed struct {
		Tools []struct {
			Name string
			Meta struct{ UI struct{ Visibility []string } } `json:"_meta"`
		}
	}
	if e = json.Unmarshal(list, &listed); e != nil {
		return e
	}
	tools := []string{"Read", "Glob", "Grep", "TodoWrite"}
	found := false
	for _, t := range listed.Tools {
		if v := t.Meta.UI.Visibility; len(v) > 0 && !slices.Contains(v, "model") {
			continue
		}
		if t.Name == "run_command" {
			found = true
		}
		tools = append(tools, "mcp__driga__"+t.Name)
	}
	if !found {
		return fmt.Errorf("run_command missing from chat MCP")
	}
	emit(map[string]any{"type": "system", "subtype": "init", "session_id": "command-fixture", "mcp_servers": []any{map[string]string{"name": "driga", "status": "connected"}}, "tools": tools})
	prompt, e := io.ReadAll(os.Stdin)
	if e != nil {
		return e
	}
	_, raw, ok := strings.Cut(string(prompt), "CHAT_COMMAND_FIXTURE\n")
	if !ok {
		return fmt.Errorf("missing fixture request")
	}
	var steps []step
	if e = json.Unmarshal([]byte(raw), &steps); e != nil {
		return e
	}
	var results []json.RawMessage
	for i, s := range steps {
		if s.Barrier != "" {
			res, e := http.Post(s.Barrier, "text/plain", nil)
			if e != nil {
				return e
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				return fmt.Errorf("barrier: %s", res.Status)
			}
			continue
		}
		if s.Replay {
			// Exercise one-shot bridge IDs while the fake agent's turn is live.
			payload := map[string]any{"conversation": server.Env["DRIGA_CHAT_CONVERSATION"], "token": server.Env["DRIGA_CHAT_TOKEN"], "tool_name": "mcp__driga__run_command", "tool_use_id": "fixture-replay", "input": s.Input}
			body, _ := json.Marshal(payload)
			h := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", server.Env["DRIGA_CHAT_SOCKET"])
			}}}
			defer h.CloseIdleConnections()
			for attempt := 0; attempt < 2; attempt++ {
				res, e := h.Post("http://driga/run-command", "application/json", bytes.NewReader(body))
				if e != nil {
					return e
				}
				b, e := io.ReadAll(res.Body)
				res.Body.Close()
				if e != nil {
					return e
				}
				if attempt == 0 && res.StatusCode != 200 || attempt == 1 && (res.StatusCode == 200 || !bytes.Contains(b, []byte("already used"))) {
					return fmt.Errorf("replay attempt %d: HTTP %d %s", attempt, res.StatusCode, b)
				}
			}
			continue
		}
		// Legacy actions need approve before claim; commands must go straight
		// through run_command, which prepares and executes in the broker.
		if s.Tool != "run_command" {
			p, e := rpc("tools/call", map[string]any{"name": "approve", "arguments": map[string]any{"tool_name": "mcp__driga__" + s.Tool, "tool_use_id": fmt.Sprint(i), "input": s.Input}})
			if e != nil {
				return e
			}
			if bytes.Contains(p, []byte(`\"behavior\":\"deny\"`)) {
				results = append(results, p)
				continue
			}
		}
		result, e := rpc("tools/call", map[string]any{"name": s.Tool, "arguments": s.Input})
		if e != nil {
			return e
		}
		results = append(results, result)
	}
	b, _ := json.Marshal(results)
	emit(map[string]any{"type": "result", "subtype": "success", "result": string(b), "is_error": false})
	return nil
}
func emit(v any) {
	if e := json.NewEncoder(os.Stdout).Encode(v); e != nil {
		panic(e)
	}
}
