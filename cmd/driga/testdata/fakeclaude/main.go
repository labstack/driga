// Command fakeclaude stands in for the claude CLI in tests. It answers
// Driga's capability probe; otherwise it does what claude does with the
// --mcp-config Driga gives it: starts that MCP server, completes the
// handshake, calls show_panels, reports whether the server connected, and
// answers with what the tool returned. It also records which process
// started it, so a test can tell the Driga service from the CLI.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("2.9.0 (Claude Code)")
		return
	}
	if len(args) == 1 && args[0] == "--help" {
		for _, flag := range strings.Fields("print mcp-config strict-mcp-config setting-sources tools allowedTools output-format verbose include-partial-messages permission-prompts permission-prompt-tool name resume") {
			fmt.Print("--" + flag + " ")
		}
		fmt.Println()
		return
	}
	parent, _ := exec.Command("ps", "-o", "comm=", "-p", fmt.Sprint(os.Getppid())).Output()
	_ = os.WriteFile(filepath.Join(os.Getenv("FAKE_CLAUDE_REC"), "parent"), parent, 0o600)
	var config struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	for i, a := range args {
		if a == "--mcp-config" && i+1 < len(args) {
			b, _ := os.ReadFile(args[i+1])
			_ = json.Unmarshal(b, &config)
		}
	}
	server := config.MCPServers["driga"]
	status, answer := "failed", "no MCP server"
	if server.Command != "" {
		status, answer = talk(server.Command, server.Args, server.Env)
	}
	emit(map[string]any{"type": "system", "subtype": "init", "session_id": "fake", "mcp_servers": []any{map[string]string{"name": "driga", "status": status}}})
	emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]string{"type": "text", "text": answer}}}})
	emit(map[string]any{"type": "result", "subtype": "success", "result": answer, "is_error": false})
}

func talk(command string, args []string, env map[string]string) (string, string) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	in, _ := cmd.StdinPipe()
	outPipe, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return "failed", err.Error()
	}
	defer cmd.Process.Kill()
	out := bufio.NewReader(outPipe)
	send := func(m map[string]any) { b, _ := json.Marshal(m); in.Write(append(b, '\n')) }
	read := func() map[string]any {
		line, err := out.ReadBytes('\n')
		var m map[string]any
		if err == nil {
			_ = json.Unmarshal(line, &m)
		}
		return m
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "fakeclaude", "version": "0"}}})
	if read()["result"] == nil {
		return "failed", "MCP initialize failed"
	}
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "show_panels", "arguments": map[string]any{"dashboard": map[string]any{"name": "Memory", "panels": []any{map[string]string{"id": "mem", "title": "Memory used", "viz": "stat", "source": "activity", "measure": "max:mem_used_bytes"}}}}}})
	b, _ := json.Marshal(read()["result"])
	return "connected", string(b)
}

func emit(m map[string]any) { b, _ := json.Marshal(m); fmt.Println(string(b)) }
