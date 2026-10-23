package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// Prompt inputs live only in memory, independently of conversations and their logs.
type Prompt struct {
	ID        string          `json:"id"`
	Session   string          `json:"session"`
	SessionID int64           `json:"session_id,omitempty"`
	Cwd       string          `json:"cwd"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func (p Prompt) InputText() string { return string(p.Input) }
func (p Prompt) Detail() string {
	var in struct {
		Command string `json:"command"`
		Path    string `json:"file_path"`
		Content string `json:"content"`
		Old     string `json:"old_string"`
		New     string `json:"new_string"`
	}
	if json.Unmarshal(p.Input, &in) != nil {
		return string(p.Input)
	}
	if in.Command != "" {
		return in.Command
	}
	if in.Path != "" {
		switch p.Tool {
		case "Write":
			return fmt.Sprintf("%s · %d bytes", in.Path, len(in.Content))
		case "Edit":
			return fmt.Sprintf("%s · %d → %d bytes", in.Path, len(in.Old), len(in.New))
		}
		return in.Path
	}
	return string(p.Input)
}

func ParsePromptAnswer(raw json.RawMessage) (string, bool, error) {
	var in struct {
		ID    string `json:"id"`
		Allow *bool  `json:"allow"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return "", false, err
	}
	var extra any
	if in.ID == "" || len(in.ID) > 128 || in.Allow == nil || decoder.Decode(&extra) != io.EOF {
		return "", false, errors.New("prompt id and allow are required")
	}
	return in.ID, *in.Allow, nil
}

type PromptDecision struct {
	Behavior string `json:"behavior,omitempty"`
	Message  string `json:"message,omitempty"`
}
type pendingPrompt struct {
	Prompt
	answer chan PromptDecision
}
type PromptClient interface {
	Prompts(context.Context) ([]Prompt, error)
	AnswerPrompt(context.Context, string, bool) error
}

// WaitPrompt owns the registration for the lifetime of the hook connection.
func (b *Broker) WaitPrompt(ctx context.Context, in Prompt) (PromptDecision, error) {
	if in.ID == "" || len(in.ID) > 128 || in.Session == "" || len(in.Session) > 256 || in.Tool == "" || len(in.Tool) > 256 || len(in.Cwd) > 4096 || len(in.Input) > MaxText || !json.Valid(in.Input) {
		return PromptDecision{}, errors.New("invalid prompt")
	}
	// One indexed lookup on registration supplies the UI's session link.
	if s, err := b.stateStore().SessionByKey("claude", in.Session); err == nil && s.Cwd == in.Cwd {
		if s.EndedAt != nil {
			return PromptDecision{}, nil
		}
		in.SessionID = s.ID
	} else {
		in.SessionID = 0
	}
	b.mu.Lock()
	if b.closed || ctx.Err() != nil {
		b.mu.Unlock()
		return PromptDecision{}, errors.New("broker unavailable")
	}
	if b.prompts == nil {
		b.prompts = map[string]*pendingPrompt{}
	}
	size := len(in.Input)
	for _, p := range b.prompts {
		size += len(p.Input)
	}
	if size > MaxBufferBytes || len(b.prompts) >= MaxMessages || b.prompts[in.ID] != nil {
		b.mu.Unlock()
		return PromptDecision{}, errors.New("prompt unavailable")
	}
	in.CreatedAt = time.Now().UTC()
	in.Input = append(json.RawMessage(nil), in.Input...)
	p := &pendingPrompt{Prompt: in, answer: make(chan PromptDecision, 1)}
	b.prompts[in.ID] = p
	ttl := b.promptTTL
	if ttl == 0 {
		ttl = ApprovalTTL
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if b.prompts[in.ID] == p {
			delete(b.prompts, in.ID)
		}
		b.mu.Unlock()
	}()
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	select {
	case out := <-p.answer:
		return out, nil
	case <-ctx.Done():
		return PromptDecision{}, ctx.Err()
	case <-b.ctx.Done():
		return PromptDecision{}, b.ctx.Err()
	case <-timer.C:
		return PromptDecision{}, nil
	}
}

// SessionEnd retires prompts immediately. This single slow ticker covers ends
// observed by the collector when a hook could not reach the broker.
func (b *Broker) watchPrompts() {
	defer b.wg.Done()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-tick.C:
			b.refreshPrompts()
		}
	}
}

func (b *Broker) refreshPrompts() {
	b.mu.Lock()
	sessions := map[string]bool{}
	for _, p := range b.prompts {
		sessions[p.Session] = true
	}
	b.mu.Unlock()
	for session := range sessions {
		s, err := b.stateStore().SessionByKey("claude", session)
		if err != nil {
			continue
		}
		b.mu.Lock()
		for id, p := range b.prompts {
			if p.Session == session && p.Cwd == s.Cwd {
				p.SessionID = s.ID
				if s.EndedAt != nil {
					delete(b.prompts, id)
					p.answer <- PromptDecision{}
				}
			}
		}
		b.mu.Unlock()
	}
}

func (b *Broker) Prompts(ctx context.Context) ([]Prompt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Prompt{}
	for _, p := range b.prompts {
		copy := p.Prompt
		copy.Input = append(json.RawMessage(nil), copy.Input...)
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (b *Broker) answerPrompt(id string, allow bool) error {
	p := b.prompts[id]
	if p == nil {
		return errors.New("prompt already answered")
	}
	delete(b.prompts, id)
	out := PromptDecision{Behavior: "deny", Message: "Denied from driga"}
	if allow {
		out = PromptDecision{Behavior: "allow"}
	}
	p.answer <- out
	return nil
}
func (b *Broker) AnswerPrompt(ctx context.Context, id string, allow bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.answerPrompt(id, allow)
}
func (b *Broker) EndPrompts(session, cwd string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, p := range b.prompts {
		if p.Session == session && p.Cwd == cwd {
			delete(b.prompts, id)
			p.answer <- PromptDecision{}
		}
	}
}

// ChatPrompts applies capture policy before the MCP child can see inputs.
func (b *Broker) ChatPrompts(in Approval) ([]Prompt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := b.approvalConversation(in); err != nil {
		return nil, err
	}
	out := []Prompt{}
	for _, p := range b.prompts {
		copy := p.Prompt
		copy.Input = nil
		if b.opts.Snippets {
			copy.Input = append(json.RawMessage(nil), p.Input...)
		}
		out = append(out, copy)
	}
	return out, nil
}

// AnswerApprovedPrompt consumes a claimed action once. The MCP child cannot
// answer directly or use a permission granted to another tool or input.
func (b *Broker) AnswerApprovedPrompt(in Approval, action string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.approvalConversation(in)
	if err != nil {
		return err
	}
	want, err := canonicalInput(in.Input)
	if err != nil {
		return err
	}
	for _, a := range c.actions {
		got, _ := canonicalInput(a.Input)
		if a.ID == action && a.token == c.token && a.claimed && a.Status == "allowed" && a.Tool == "mcp__driga__answer_prompt" && in.Tool == a.Tool && got == want {
			id, allow, err := ParsePromptAnswer(a.Input)
			if err != nil {
				return err
			}
			err = b.answerPrompt(id, allow)
			status, result := "done", "Prompt answered"
			if err != nil {
				status, result = "failed", err.Error()
			}
			b.finishAction(c, action, status, result)
			return err
		}
	}
	return errors.New("approved prompt action not found")
}
func (c *Remote) Prompts(ctx context.Context) ([]Prompt, error) {
	var out []Prompt
	err := c.call(ctx, "GET", "/prompts", nil, &out)
	return out, err
}
func (c *Remote) AnswerPrompt(ctx context.Context, id string, allow bool) error {
	return c.call(ctx, "POST", "/prompt-answer", map[string]any{"id": id, "allow": allow}, nil)
}
func (c *Remote) WaitPrompt(ctx context.Context, in Prompt) (PromptDecision, error) {
	var out PromptDecision
	err := c.callOnce(ctx, "POST", "/hook-prompt", in, &out, false)
	return out, err
}
func (c *Remote) EndPrompts(ctx context.Context, session, cwd string) error {
	return c.call(ctx, "POST", "/hook-end", map[string]string{"session": session, "cwd": cwd}, nil)
}
func ChatPrompts(ctx context.Context, path string, in Approval) ([]Prompt, error) {
	c, err := dialPath(ctx, path)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var out []Prompt
	err = c.call(ctx, "POST", "/chat-prompts", in, &out)
	return out, err
}
func AnswerApprovedPrompt(ctx context.Context, path string, in Approval, action string) error {
	c, err := dialPath(ctx, path)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.call(ctx, "POST", "/answer-approved-prompt", struct {
		Approval
		ID string
	}{in, action}, nil)
}
