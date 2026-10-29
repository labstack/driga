package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"strings"

	"github.com/labstack/driga/internal/actions"

	"github.com/labstack/driga/internal/chat"
)

func chatApproval(raw json.RawMessage) (chat.Approval, error) {
	var in chat.Approval
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return in, err
	}
	in.Conversation, in.Token = os.Getenv("DRIGA_CHAT_CONVERSATION"), os.Getenv("DRIGA_CHAT_TOKEN")
	if in.Conversation == "" || in.Token == "" || os.Getenv("DRIGA_CHAT_SOCKET") == "" {
		return in, errors.New("Chat turn bridge unavailable")
	}
	return in, nil
}
func chatApprove(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	in, err := chatApproval(raw)
	var answer chat.Permission
	if err == nil {
		answer, err = chat.Bridge(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), in)
	}
	if err != nil {
		answer = chat.Permission{Behavior: "deny", Message: err.Error()}
	}
	return json.Marshal(answer)
}

func chatAction(base *Globals) func(context.Context, string, json.RawMessage) (string, error) {
	return func(ctx context.Context, name string, raw json.RawMessage) (string, error) {
		request, _ := json.Marshal(map[string]any{"tool_name": "mcp__driga__" + name, "input": raw})
		approval, err := chatApproval(request)
		if err != nil {
			return "", err
		}
		if name == "run_command" {
			approval.ToolUseID = uuid.NewString()
			result, err := chat.RunCommand(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), approval)
			raw, encodeErr := json.Marshal(result)
			return string(raw), errors.Join(err, encodeErr)
		}
		action, err := chat.Claim(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), approval)
		if err != nil {
			return "", err
		}
		if name == "answer_prompt" {
			err = chat.AnswerApprovedPrompt(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), approval, action)
			if err != nil {
				return "", err
			}
			return "Prompt answered", nil
		}
		var out bytes.Buffer
		g := *base
		g.Machine = "local"
		g.Stdout = &out
		g.Stdin = nil
		g.JSON = false
		err = runChatAction(ctx, &g, name, raw)
		result := strings.TrimSpace(out.String())
		if err != nil {
			result = err.Error()
		}
		if len(result) > 1024 {
			result = result[:1024]
		}
		logErr := chat.Complete(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), approval, action, result, err != nil)
		return result, errors.Join(err, logErr)
	}
}
func runChatAction(ctx context.Context, g *Globals, name string, raw json.RawMessage) error {
	var in struct {
		ID        string `json:"id"`
		SessionID int64  `json:"session_id"`
		All       bool   `json:"all"`
		Force     bool   `json:"force"`
		Project   string `json:"project"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch name {
	case "ack_inbox":
		return (InboxAckCmd{ID: in.ID, All: in.All}).Run(g)
	case "cleanup":
		if in.ID == "" {
			return errors.New("candidate id required")
		}
		return (CleanupRemoveCmd{ID: in.ID, Yes: true}).Run(g)
	case "summarize":
		if in.Project == "" {
			return errors.New("project required")
		}
		return (SummarizeCmd{Projects: []string{in.Project}}).run(ctx, g)
	case "kill":
		if in.SessionID > 0 && in.ID != "" {
			return errors.New("provide a session or orphan id")
		}
		if in.SessionID > 0 {
			return actions.KillSession(ctx, actionEnvironment(g, true), in.SessionID, in.Force)
		}
		return (OrphansKillCmd{ID: in.ID, Force: in.Force, Yes: true}).run(ctx, g)
	case "jump":
		return actions.JumpSession(ctx, actionEnvironment(g, true), newInboxRunner(), in.SessionID)
	}
	return errors.New("unknown Chat action")
}
