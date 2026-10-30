package cli

import (
	"context"
	"encoding/json"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/config"
)

type permissionPayload struct {
	Session    string          `json:"session_id"`
	Cwd        string          `json:"cwd"`
	Transcript string          `json:"transcript_path"`
	Tool       string          `json:"tool_name"`
	Input      json.RawMessage `json:"tool_input"`
}

func promptBroker() (*chat.Remote, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	return chat.DialHook(ctx, config.DataDir())
}
func endPromptSession(session, cwd string) {
	c, err := promptBroker()
	if err != nil {
		return
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = c.EndPrompts(ctx, session, cwd)
}
func (h HookCmd) permission(g *Globals) error {
	started := time.Now()
	if g == nil || g.Stdin == nil {
		return nil
	}
	// Drain a bounded payload even when no collector or output is available:
	// exiting before reading can break the agent's pipe with EPIPE.
	data, err := hookReadInput(g.Stdin)
	if err != nil || g.Stdout == nil {
		return nil
	}
	c, err := promptBroker()
	if err != nil {
		return nil
	}
	defer c.Close()
	var in permissionPayload
	if json.Unmarshal(data, &in) != nil || in.Session == "" || in.Tool == "" || !json.Valid(in.Input) {
		return nil
	}
	out, err := chat.WaitPermission(context.Background(), c, chat.PermissionRequest{Session: in.Session, Cwd: in.Cwd, Transcript: in.Transcript, Tool: in.Tool, Input: in.Input}, started, openTranscript)
	if err != nil || (out.Behavior != "allow" && out.Behavior != "deny") {
		return nil
	}
	_ = json.NewEncoder(g.Stdout).Encode(struct {
		Output any `json:"hookSpecificOutput"`
	}{struct {
		Event    string              `json:"hookEventName"`
		Decision chat.PromptDecision `json:"decision"`
	}{"PermissionRequest", out}})
	return nil
}
