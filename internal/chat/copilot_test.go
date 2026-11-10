package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type copilotFixtureTools struct{ calls int }

func (b *copilotFixtureTools) Catalog() []providerTool {
	return []providerTool{{Name: "get_summary", Description: "Read summary", InputSchema: json.RawMessage(`{"type":"object"}`)}}
}
func (b *copilotFixtureTools) Call(ctx context.Context, name string, args json.RawMessage) (providerToolResult, error) {
	b.calls++
	if ctx.Err() != nil {
		return providerToolResult{}, ctx.Err()
	}
	if name != "get_summary" || string(args) != `{}` {
		return providerToolResult{}, fmt.Errorf("wrong call")
	}
	return providerToolResult{Text: "fixture summary"}, nil
}
func (b *copilotFixtureTools) Close() error { return nil }

func fakeCopilot(t *testing.T, mode string) (*Copilot, *copilotFixtureTools, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable shell fixture")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "copilot")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec \"$COPILOT_FIXTURE_EXE\" -test.run=^TestCopilotRuntimeFixture$\n"), 0700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "requests")
	t.Setenv("COPILOT_FIXTURE_EXE", executable)
	t.Setenv("COPILOT_FIXTURE_MODE", mode)
	t.Setenv("COPILOT_FIXTURE_RECORD", record)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("COPILOT_HOME", t.TempDir())
	bridge := &copilotFixtureTools{}
	a := &Copilot{Path: path, DataDir: t.TempDir(), openTools: func(context.Context, string, string, Request) (copilotTools, error) { return bridge, nil }}
	return a, bridge, record
}

// The subprocess implements protocol metadata and canned events only. It never
// invokes an installed agent, authentication flow or model.
func TestCopilotRuntimeFixture(t *testing.T) {
	mode := os.Getenv("COPILOT_FIXTURE_MODE")
	if mode == "" {
		return
	}
	log, err := os.OpenFile(os.Getenv("COPILOT_FIXTURE_RECORD"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(20)
	}
	p := copilotRPC{reader: bufio.NewReaderSize(os.Stdin, 4096), writer: os.Stdout}
	session := "fixture-session"
	respond := func(m copilotMessage, value any) {
		if p.write(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": value}) != nil {
			os.Exit(21)
		}
	}
	event := func(kind string, data any) {
		eventSession := session
		if mode == "foreign" {
			eventSession = "foreign-session"
		}
		if p.write(map[string]any{"jsonrpc": "2.0", "method": "session.event", "params": map[string]any{"sessionId": eventSession, "event": map[string]any{"type": kind, "data": data}}}) != nil {
			os.Exit(22)
		}
	}
	tool := func() {
		event("external_tool.requested", map[string]any{"requestId": "request-1", "toolCallId": "call-1", "toolName": "get_summary", "arguments": map[string]any{}})
	}
	for {
		m, err := p.read()
		if err != nil {
			os.Exit(0)
		}
		json.NewEncoder(log).Encode(m)
		switch m.Method {
		case "connect":
			version := 3
			if mode == "protocol" {
				version = 2
			}
			respond(m, map[string]any{"protocolVersion": version})
		case "session.create", "session.resume":
			var options struct {
				SessionID string `json:"sessionId"`
			}
			json.Unmarshal(m.Params, &options)
			session = options.SessionID
			respond(m, map[string]any{"sessionId": session})
		case "session.options.update":
			ack := map[string]any{"success": mode != "options", "pluginHookCount": 0}
			if mode == "hooks-missing" {
				delete(ack, "pluginHookCount")
			}
			if mode == "hooks-null" {
				ack["pluginHookCount"] = nil
			}
			if mode == "hooks-active" {
				ack["pluginHookCount"] = 1
			}
			respond(m, ack)
		case "session.tools.initializeAndValidate":
			respond(m, map[string]any{})
		case "session.tools.getCurrentMetadata":
			name := "get_summary"
			if mode == "native" {
				name = "bash"
			}
			respond(m, map[string]any{"tools": []any{map[string]string{"name": name}}})
		case "session.send":
			respond(m, map[string]any{"messageId": "message-1"})
			if mode == "cancel" {
				continue
			}
			if mode == "tool" || mode == "duplicate" {
				tool()
				continue
			}
			if mode == "permission" {
				event("permission.requested", map[string]any{"requestId": "native-1"})
				continue
			}
			event("assistant.message_delta", map[string]string{"deltaContent": "answer"})
			event("assistant.message", map[string]string{"content": "answer"})
			event("session.idle", map[string]any{})
		case "session.tools.handlePendingToolCall":
			respond(m, map[string]bool{"success": true})
			if mode == "duplicate" {
				tool()
				continue
			}
			event("assistant.message", map[string]string{"content": "used summary"})
			event("session.idle", map[string]any{})
		default:
			os.Exit(23)
		}
	}
}

func TestCopilotIsolatedTurnAndResume(t *testing.T) {
	a, _, record := fakeCopilot(t, "success")
	t.Setenv("DRIGA_CHAT_TOKEN", "must-not-reach-runtime")
	var text, session string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := a.Run(ctx, Request{Prompt: "question", Resume: "fixture-session"}, func(d Delta) error {
		text += d.Text
		if d.SessionID != "" {
			session = d.SessionID
		}
		return nil
	})
	if err != nil || text != "answer" || session != "fixture-session" {
		t.Fatalf("text=%q session=%q err=%v", text, session, err)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	var requests []copilotMessage
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	for {
		var m copilotMessage
		if err := decoder.Decode(&m); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, m)
	}
	if len(requests) != 6 || requests[1].Method != "session.resume" || requests[5].Method != "session.send" {
		t.Fatalf("request sequence: %s", raw)
	}
	var options map[string]any
	json.Unmarshal(requests[1].Params, &options)
	for _, key := range []string{"enableConfigDiscovery", "enableFileHooks", "enableHostGitOperations", "enableSkills", "enableSessionStore", "enableOnDemandInstructionDiscovery"} {
		if options[key] != false {
			t.Fatalf("unsafe %s=%v", key, options[key])
		}
	}
	if options["configDir"] == os.Getenv("COPILOT_HOME") || options["workingDirectory"] == os.Getenv("HOME") || options["sessionId"] != "fixture-session" {
		t.Fatal(options)
	}
	if got := fmt.Sprint(options["availableTools"]); got != "[get_summary]" {
		t.Fatal(got)
	}
}

func TestCopilotFailsBeforePromptWithoutSafetyCeiling(t *testing.T) {
	for _, mode := range []string{"protocol", "options", "native", "hooks-missing", "hooks-null", "hooks-active"} {
		t.Run(mode, func(t *testing.T) {
			a, _, record := fakeCopilot(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := a.Run(ctx, Request{Prompt: "private question"}, func(Delta) error { return nil }); err == nil {
				t.Fatal("unsafe runtime accepted")
			}
			raw, _ := os.ReadFile(record)
			if strings.Contains(string(raw), "session.send") || strings.Contains(string(raw), "private question") {
				t.Fatal("prompt sent before safety validation")
			}
		})
	}
}

func TestCopilotHostToolsAndUnexpectedAuthority(t *testing.T) {
	for _, mode := range []string{"tool", "duplicate", "foreign", "permission"} {
		t.Run(mode, func(t *testing.T) {
			a, bridge, _ := fakeCopilot(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := a.Run(ctx, Request{Prompt: "question"}, func(Delta) error { return nil })
			if (err == nil) != (mode == "tool") {
				t.Fatalf("mode %s err=%v", mode, err)
			}
			want := 0
			if mode == "tool" || mode == "duplicate" {
				want = 1
			}
			if bridge.calls != want {
				t.Fatalf("tool calls=%d want=%d", bridge.calls, want)
			}
		})
	}
}

func TestCopilotCancellation(t *testing.T) {
	a, _, _ := fakeCopilot(t, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, Request{Prompt: "question"}, func(d Delta) error {
			if d.SessionID != "" {
				cancel()
			}
			return nil
		})
	}()
	defer cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled turn succeeded")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("provider did not stop")
	}
}

func TestCopilotRejectsOversizedAndMalformedFrames(t *testing.T) {
	for _, raw := range []string{"Content-Length: 1048577\r\n\r\n", strings.Repeat("x", 8192), "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", "Content-Length: 2\r\n\r\n{}"} {
		p := copilotRPC{reader: bufio.NewReaderSize(strings.NewReader(raw), 4096)}
		if _, err := p.read(); err == nil {
			t.Fatal("malformed frame accepted")
		}
	}
}
