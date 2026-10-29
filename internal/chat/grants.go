package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/labstack/driga/internal/command"
	"github.com/labstack/driga/internal/model"
	"path/filepath"
	"runtime"
	"slices"
	"time"
)

const GrantTTL = 30 * time.Minute
const grantAttribution = "allowed by your grant"

type Grant struct {
	ID           string              `json:"id"`
	Conversation string              `json:"conversation,omitempty"`
	Session      string              `json:"session,omitempty"`
	Cwd          string              `json:"cwd,omitempty"`
	Family       command.Class       `json:"family"`
	Pattern      command.ArgvPattern `json:"pattern"`
	CreatedAt    time.Time           `json:"created_at"`
	ExpiresAt    time.Time           `json:"expires_at"`
	sessionEpoch string
}
type GrantDecision struct {
	GrantID      string    `json:"grant_id"`
	ActionID     string    `json:"action_id,omitempty"`
	PromptID     string    `json:"prompt_id,omitempty"`
	Conversation string    `json:"conversation,omitempty"`
	Session      string    `json:"session,omitempty"`
	Time         time.Time `json:"time"`
	Attribution  string    `json:"attribution"`
	Revoked      bool      `json:"revoked,omitempty"`
}
type GrantClient interface {
	DecideSimilar(context.Context, string, string) error
	AnswerPromptSimilar(context.Context, string) error
	RevokeGrant(context.Context, string) error
	GrantHistory(context.Context) ([]GrantDecision, error)
}
type sessionScope struct {
	identity string
	epoch    string
	id       int64
}
type sessionKey struct{ session, cwd string }

func (b *Broker) now() time.Time {
	if b.opts.Now != nil {
		return b.opts.Now().UTC()
	}
	return time.Now().UTC()
}

// All grant operations and authority consumption share b.mu.
func (b *Broker) addGrant(g Grant) {
	if b.grants == nil {
		b.grants = map[string]Grant{}
	}
	b.pruneGrants()
	b.grants[g.ID] = g
}
func (b *Broker) pruneGrants() {
	for id, g := range b.grants {
		if !b.now().Before(g.ExpiresAt) {
			delete(b.grants, id)
		}
	}
}
func (b *Broker) matchingGrant(p command.Prepared, conversation, epoch string) *Grant {
	b.pruneGrants()
	for _, g := range b.grants {
		if (conversation != "" && g.Conversation == conversation || epoch != "" && g.sessionEpoch == epoch) && command.MatchPattern(g.Pattern, p) {
			cp := g
			return &cp
		}
	}
	return nil
}
func (b *Broker) grantDecision(g Grant, action, prompt string) {
	b.grantHistory = append(b.grantHistory, GrantDecision{GrantID: g.ID, ActionID: action, PromptID: prompt, Conversation: g.Conversation, Session: g.Session, Time: b.now(), Attribution: grantAttribution})
	if len(b.grantHistory) > MaxMessages {
		b.grantHistory = append([]GrantDecision(nil), b.grantHistory[len(b.grantHistory)-MaxMessages:]...)
	}
}
func (b *Broker) DecideSimilar(ctx context.Context, conversation, action string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.conversations[conversation]
	if b.closed || c == nil || c.cancel == nil || c.readOnly || c.remote {
		return errors.New("conversation no longer active")
	}
	for _, a := range c.actions {
		if a.ID != action || a.Status != "pending" {
			continue
		}
		if !a.GrantEligible || a.prepared == nil || a.digest != command.Digest(*a.prepared) || !b.now().Before(a.GrantExpiresAt) {
			return errors.New("grant offer unavailable")
		}
		pattern, err := command.NewArgvPattern(*a.prepared)
		if err != nil || pattern.String() != a.GrantPattern {
			return errors.New("grant offer changed")
		}
		if len(b.grants) >= MaxMessages {
			return errors.New("too many grants")
		}
		g := Grant{ID: uuid.NewString(), Conversation: conversation, Family: pattern.Family, Pattern: pattern, CreatedAt: b.now(), ExpiresAt: a.GrantExpiresAt}
		b.addGrant(g)
		a.GrantID = g.ID
		if err = b.resolve(c, a, true, "you", ""); err != nil {
			delete(b.grants, g.ID)
			return err
		}
		return nil
	}
	return errors.New("pending action not found")
}
func (b *Broker) RevokeGrant(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.grants[id]; !ok {
		return errors.New("grant unavailable")
	}
	delete(b.grants, id)
	for i := range b.grantHistory {
		if b.grantHistory[i].GrantID == id {
			b.grantHistory[i].Revoked = true
		}
	}
	return nil
}
func (b *Broker) GrantHistory(ctx context.Context) ([]GrantDecision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneGrants()
	out := append([]GrantDecision{}, b.grantHistory...)
	for i := range out {
		if _, ok := b.grants[out[i].GrantID]; !ok {
			out[i].Revoked = true
		}
	}
	return out, nil
}
func (b *Broker) retireConversationGrants(id string) {
	for key, g := range b.grants {
		if g.Conversation == id {
			delete(b.grants, key)
		}
	}
}
func (b *Broker) retireSessionGrants(session, cwd string) {
	key := sessionKey{session, cwd}
	epoch := b.sessionScopes[key].epoch
	delete(b.sessionScopes, key)
	for id, g := range b.grants {
		if g.sessionEpoch == epoch && epoch != "" {
			delete(b.grants, id)
		}
	}
}
func (b *Broker) promptEpoch(p Prompt, identity string) string {
	if b.sessionScopes == nil {
		b.sessionScopes = map[sessionKey]sessionScope{}
	}
	key := sessionKey{p.Session, p.Cwd}
	scope := b.sessionScopes[key]
	if scope.epoch == "" || scope.identity != identity || scope.id != 0 && p.SessionID != 0 && scope.id != p.SessionID {
		b.retireSessionGrants(p.Session, p.Cwd)
		scope = sessionScope{epoch: uuid.NewString()}
	}
	if p.SessionID != 0 {
		scope.id = p.SessionID
	}
	scope.identity = identity
	b.sessionScopes[key] = scope
	return scope.epoch
}

// Hook grants support only classified, single literal command inputs. Exact
// script text with expansion, redirection, wrappers or unsupported tools has no offer.
func (b *Broker) preparePromptGrant(ctx context.Context, p Prompt) *command.Prepared {
	if p.SessionID == 0 {
		return nil
	} // No grant without a trusted session lifetime.
	var req command.Request
	switch p.Tool {
	case "Bash":
		var in struct {
			Command     string `json:"command"`
			Description string `json:"description"`
			Timeout     int    `json:"timeout"`
		}
		d := json.NewDecoder(bytes.NewReader(p.Input))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil {
			return nil
		}
		parsed, err := command.Parse(command.Request{Script: in.Command, Cwd: p.Cwd})
		if err != nil || parsed.Complex || parsed.Pipeline || len(parsed.Commands) != 1 {
			return nil
		}
		req = command.Request{Argv: parsed.Commands[0], Cwd: p.Cwd}
	case "run_command":
		var err error
		req, err = decodeCommand(p.Input)
		if err != nil || req.Script != "" {
			return nil
		}
		req.Cwd = p.Cwd
	default:
		return nil
	}
	root, err := filepath.EvalSymlinks(p.Cwd)
	if err != nil || !filepath.IsAbs(root) {
		return nil
	}
	req.Cwd = root
	// Scratch is a private broker directory, not supplied by tool input.
	prepared, err := command.Prepare(ctx, req, command.Scope{Project: root, Scratch: b.grantScratch, Denied: []string{b.opts.DataDir}})
	if err != nil {
		return nil
	}
	// A hook returns only allow: the external agent keeps its own environment
	// and configuration. Only these reads are eligible, and only after the
	// classifier validates their executable and flag grammar. Other reads (e.g.
	// Git and ripgrep) can execute helpers through external configuration.
	nativeRead := runtime.GOOS == "windows" && p.Tool == "run_command" && len(req.Argv) > 0 && req.Argv[0] == "type"
	if prepared.Policy.Class != command.Read || len(req.Argv) == 0 || !nativeRead && !slices.Contains([]string{"ls", "cat", "head", "tail", "wc", "stat", "du", "df", "pwd"}, req.Argv[0]) {
		return nil
	}
	if len(prepared.Policy.Commands) != 1 || !slices.Equal(prepared.Policy.Commands[0].Argv, req.Argv) {
		return nil
	}
	if _, err = command.NewArgvPattern(prepared); err != nil {
		return nil
	}
	return &prepared
}
func (b *Broker) AnswerPromptSimilar(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.prompts[id]
	if p == nil || p.wait.Err() != nil || !p.GrantEligible || p.grantPrepared == nil || p.grantDigest != promptDigest(p.Prompt) || !b.now().Before(p.GrantExpiresAt) {
		return errors.New("grant offer unavailable")
	}
	pattern, err := command.NewArgvPattern(*p.grantPrepared)
	if err != nil || pattern.String() != p.GrantPattern {
		return errors.New("grant offer changed")
	}
	if len(b.grants) >= MaxMessages {
		return errors.New("too many grants")
	}
	g := Grant{ID: uuid.NewString(), Session: p.Session, Cwd: p.Cwd, Family: pattern.Family, Pattern: pattern, CreatedAt: b.now(), ExpiresAt: p.GrantExpiresAt, sessionEpoch: p.sessionEpoch}
	b.addGrant(g)
	if err = b.answerPrompt(id, true); err != nil {
		delete(b.grants, g.ID)
	}
	return err
}
func (c *Remote) DecideSimilar(ctx context.Context, conversation, action string) error {
	return c.callOnce(ctx, "POST", "/decide", map[string]any{"id": conversation, "action": action, "allow": true, "similar": true}, nil, false)
}
func (c *Remote) AnswerPromptSimilar(ctx context.Context, id string) error {
	return c.callOnce(ctx, "POST", "/prompt-answer", map[string]any{"id": id, "allow": true, "similar": true}, nil, false)
}
func (c *Remote) RevokeGrant(ctx context.Context, id string) error {
	return c.callOnce(ctx, "POST", "/grant-revoke", map[string]string{"id": id}, nil, false)
}
func (c *Remote) GrantHistory(ctx context.Context) ([]GrantDecision, error) {
	var out []GrantDecision
	err := c.call(ctx, "GET", "/grant-history", nil, &out)
	return out, err
}

// A database ID or external session key alone can be reused. Bind grants and
// groups to the collected session/process lifetime as well.
func sessionLifetime(s model.Session) string {
	if s.StartedAt.IsZero() && s.ProcStart.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d/%d/%d/%d", s.ID, s.StartedAt.UnixNano(), s.PID, s.ProcStart.UnixNano())
}
