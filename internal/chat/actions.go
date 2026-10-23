package chat

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/store"
)

type Mode string

const (
	Confirm  Mode = "confirm"
	Auto     Mode = "auto"
	FullAuto Mode = "full auto"
)
const ApprovalTTL = 10 * time.Minute

// FullAutoTTL keeps unattended runs short: Chat reads transcripts, files and
// pages that may try to steer it, and Full auto shows no cards.
const FullAutoTTL = 30 * time.Minute

type Approval struct {
	Conversation string          `json:"conversation"`
	Token        string          `json:"token"`
	Tool         string          `json:"tool_name"`
	Input        json.RawMessage `json:"input"`
	ToolUseID    string          `json:"tool_use_id"`
}
type Permission struct {
	Behavior     string          `json:"behavior"`
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"`
	Message      string          `json:"message,omitempty"`
}
type Action struct {
	ID        string          `json:"id"`
	Tool      string          `json:"tool"`
	Title     string          `json:"title"`
	Input     json.RawMessage `json:"input"`
	Directory string          `json:"directory"`
	Status    string          `json:"status"`
	DecidedBy string          `json:"decided_by,omitempty"`
	Result    string          `json:"result,omitempty"`
}
type pendingAction struct {
	Action
	token     string
	toolUseID string
	answer    chan Permission
	claimed   bool
}

func (a *pendingAction) awaitingResult() bool {
	return a.Status == "allowed" || a.Status == "stopped" && a.claimed
}

func (b *Broker) stateStore() *store.Store {
	if b.opts.ContextStore != nil {
		return b.opts.ContextStore
	}
	return b.st
}
func (b *Broker) SetMode(ctx context.Context, id string, mode Mode) error {
	if mode != Confirm && mode != Auto && mode != FullAuto {
		return errors.New("invalid trust mode")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.load(id)
	if err != nil {
		return err
	}
	if c.readOnly {
		return errors.New("driga ask stays read-only")
	}
	b.setMode(c, mode)
	return nil
}

// setMode is called with b.mu held. Full auto always starts a fresh window.
func (b *Broker) setMode(c *conversation, mode Mode) {
	if c.expire != nil {
		c.expire.Stop()
		c.expire = nil
	}
	c.mode, c.until = mode, time.Time{}
	if mode == FullAuto {
		ttl := b.fullAutoTTL
		if ttl == 0 {
			ttl = FullAutoTTL
		}
		c.until = time.Now().Add(ttl)
		c.expire = time.AfterFunc(ttl, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.expireFullAuto(c)
		})
	}
	b.appendEvent(c, Event{Kind: "mode", Mode: c.mode, Until: c.until})
}

// expireFullAuto is called with b.mu held.
func (b *Broker) expireFullAuto(c *conversation) {
	if c.mode == FullAuto && !time.Now().Before(c.until) {
		b.setMode(c, Auto)
	}
}
func (b *Broker) Decide(ctx context.Context, id, action string, allow bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.load(id)
	if err != nil {
		return err
	}
	for _, a := range c.actions {
		if a.ID == action && a.Status == "pending" {
			return b.resolve(c, a, allow, "you", "")
		}
	}
	return errors.New("pending action not found")
}
func (b *Broker) actionEvent(c *conversation, a *pendingAction) error {
	a.Title = scrub.Snippet(strings.Join(strings.Fields(a.Title), " "))
	a.Result = scrub.Snippet(strings.Join(strings.Fields(a.Result), " "))
	title := a.Title
	if a.Tool == "mcp__driga__answer_prompt" {
		// Another session's prompt input stays in memory, including its card title.
		id, _, _ := ParsePromptAnswer(a.Input)
		title = "answer_prompt " + id
	}
	m := store.ChatMessage{ID: a.ID, MachineID: c.row.MachineID, ConversationID: c.row.ID, Role: "action", Tool: scrub.Snippet(a.Tool), DecidedBy: a.DecidedBy, Text: title + " · " + a.Status, Status: a.Status, CreatedAt: time.Now().UTC()}
	if a.DecidedBy != "" {
		m.Text += " · " + a.DecidedBy
	}
	if a.Result != "" {
		m.Text += " · " + a.Result
	}
	if err := b.save(context.Background(), m); err != nil {
		return err
	}
	b.updateMessage(c, m)
	copy := a.Action
	b.appendEvent(c, Event{Kind: "action", Message: &m, Action: &copy})
	return nil
}
func (b *Broker) resolve(c *conversation, a *pendingAction, allow bool, who, reason string) error {
	if a.answer == nil {
		allow, reason = false, "interrupted"
	}
	a.Status, a.DecidedBy, a.Result = "denied", who, reason
	p := Permission{Behavior: "deny", Message: reason}
	if p.Message == "" {
		p.Message = "Denied by user"
	}
	if allow {
		a.Status = "allowed"
		p = Permission{Behavior: "allow", UpdatedInput: append(json.RawMessage(nil), a.Input...)}
	}
	if reason == "expired" {
		a.Status = "expired"
	}
	err := b.actionEvent(c, a)
	if err != nil {
		p = Permission{Behavior: "deny", Message: "Could not retain action log"}
		a.Status = "denied"
	}
	if a.answer != nil {
		a.answer <- p
	}
	return err
}
func (b *Broker) denyPending(c *conversation, reason string) {
	for _, a := range c.actions {
		if a.Status == "pending" {
			_ = b.resolve(c, a, false, "", reason)
		}
	}
}
func (b *Broker) approvalConversation(in Approval) (*conversation, error) {
	c := b.conversations[in.Conversation]
	if b.closed || c == nil || c.cancel == nil || c.token == "" || subtle.ConstantTimeCompare([]byte(c.token), []byte(in.Token)) != 1 || c.readOnly {
		return nil, errors.New("invalid turn token or conversation")
	}
	return c, nil
}
func (b *Broker) Approve(ctx context.Context, in Approval) (Permission, error) {
	if len(in.Input) > MaxText || !json.Valid(in.Input) || len(in.Tool) > 256 || in.Tool == "" || len(in.ToolUseID) > 256 {
		return Permission{}, errors.New("invalid approval request")
	}
	b.mu.Lock()
	c, err := b.approvalConversation(in)
	if err != nil {
		b.mu.Unlock()
		return Permission{}, err
	}
	directory, project := c.directory, c.autoProject
	b.mu.Unlock()
	title, remote := b.actionTitle(in), b.remoteTarget(in)
	auto := autoAllowed(in.Tool, in.Input, directory, project)
	b.mu.Lock()
	c, err = b.approvalConversation(in)
	if err != nil {
		b.mu.Unlock()
		return Permission{}, err
	}
	// Historical cards must never consume the outstanding-work budget.
	// Their audit messages remain in storage after leaving this live window.
	size := func(a *pendingAction) int { return len(a.Input) + len(a.Title) + len(a.Directory) + len(a.Result) }
	bytes := len(in.Input) + len(title) + len(c.directory)
	for _, a := range c.actions {
		bytes += size(a)
	}
	for i := 0; i < len(c.actions) && (len(c.actions) >= MaxMessages || bytes > MaxBufferBytes); {
		a := c.actions[i]
		if a.Status == "pending" || a.awaitingResult() {
			i++
			continue
		}
		bytes -= size(a)
		copy(c.actions[i:], c.actions[i+1:])
		c.actions[len(c.actions)-1] = nil
		c.actions = c.actions[:len(c.actions)-1]
	}
	if len(c.actions) >= MaxMessages || bytes > MaxBufferBytes {
		b.mu.Unlock()
		return Permission{}, errors.New("too many actions in conversation")
	}
	a := &pendingAction{Action: Action{ID: uuid.NewString(), Tool: in.Tool, Title: title, Input: append(json.RawMessage(nil), in.Input...), Directory: c.directory, Status: "pending"}, token: c.token, toolUseID: in.ToolUseID, answer: make(chan Permission, 1)}
	c.actions = append(c.actions, a)
	b.expireFullAuto(c)
	if c.remote || remote {
		err = b.resolve(c, a, false, "", "Actions on another machine are unavailable; run driga there")
	} else if c.mode == FullAuto {
		err = b.resolve(c, a, true, "full auto", "")
	} else if c.mode == Auto && auto {
		err = b.resolve(c, a, true, "auto", "")
	} else {
		err = b.actionEvent(c, a)
	}
	b.mu.Unlock()
	if err != nil {
		return Permission{}, err
	}
	ttl := b.approvalTTL
	if ttl == 0 {
		ttl = ApprovalTTL
	}
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	select {
	case p := <-a.answer:
		return p, nil
	case <-timer.C:
	case <-ctx.Done():
	case <-b.ctx.Done():
	}
	b.mu.Lock()
	if a.Status == "pending" {
		reason := "expired"
		if ctx.Err() != nil || b.ctx.Err() != nil {
			reason = "stopped"
		}
		_ = b.resolve(c, a, false, "", reason)
	}
	b.mu.Unlock()
	return <-a.answer, nil
}
func (b *Broker) finishAction(c *conversation, toolUseID, status, result string) {
	for _, a := range c.actions {
		if a.token == c.token && (a.toolUseID == toolUseID || a.ID == toolUseID) && a.Status == "allowed" {
			a.Status, a.Result = status, result
			_ = b.actionEvent(c, a)
			return
		}
	}
}
func (b *Broker) Claim(in Approval) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.approvalConversation(in)
	if err != nil {
		return "", err
	}
	want, err := canonicalInput(in.Input)
	if err != nil {
		return "", err
	}
	for _, a := range c.actions {
		got, err := canonicalInput(a.Input)
		if err != nil {
			continue
		}
		if a.token == c.token && a.Status == "allowed" && !a.claimed && a.Tool == in.Tool && want == got {
			a.claimed = true
			return a.ID, nil
		}
	}
	return "", errors.New("action has no approved permission")
}
func (b *Broker) Complete(in Approval, id, result string, failed bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// A claimed action may finish after Stop or after a new turn starts.
	// Only its own token authorizes this result; it grants no new permission.
	c := b.conversations[in.Conversation]
	if b.closed || c == nil || in.Token == "" {
		return errors.New("invalid action token or conversation")
	}
	status := "done"
	if failed {
		status = "failed"
	}
	for _, a := range c.actions {
		if a.ID == id && subtle.ConstantTimeCompare([]byte(a.token), []byte(in.Token)) == 1 && a.claimed && (a.Status == "allowed" || a.Status == "stopped") {
			a.Status, a.Result = status, result
			return b.actionEvent(c, a)
		}
	}
	return errors.New("claimed action not found")
}
func (b *Broker) actionTitle(in Approval) string {
	var args struct {
		ID        string `json:"id"`
		SessionID int64  `json:"session_id"`
		Project   string `json:"project"`
		All       bool   `json:"all"`
		Allow     bool   `json:"allow"`
	}
	_ = json.Unmarshal(in.Input, &args)
	name := strings.TrimPrefix(in.Tool, "mcp__driga__")
	if name == "answer_prompt" {
		decision := "Deny"
		if args.Allow {
			decision = "Allow"
		}
		b.mu.Lock()
		p := b.prompts[args.ID]
		var prompt Prompt
		if p != nil {
			prompt = p.Prompt
		}
		b.mu.Unlock()
		if p != nil {
			session := prompt.Session
			if prompt.SessionID > 0 {
				session = fmt.Sprintf("#%d (%s)", prompt.SessionID, session)
			}
			return fmt.Sprintf("%s · claude %s · %s · %s · %s", decision, session, prompt.Tool, prompt.Detail(), prompt.Cwd)
		}
		return fmt.Sprintf("%s prompt %s · unavailable", decision, args.ID)
	}
	if args.SessionID > 0 && (name == "kill" || name == "jump") {
		if s, err := b.stateStore().SessionScoped(args.SessionID, store.Scope{}); err == nil {
			return fmt.Sprintf("%s %s #%d · %s", name, s.Adapter, s.ID, s.Cwd)
		}
	}
	if strings.HasPrefix(in.Tool, "mcp__driga__") {
		target := args.ID
		if name == "kill" && target != "" {
			items, _ := b.stateStore().Orphans()
			for _, o := range items {
				if o.ID == target {
					return fmt.Sprintf("Kill %s · %s · %d processes", o.ID, o.Cwd, o.ProcCount)
				}
			}
		}
		if name == "ack_inbox" && target != "" {
			items, _ := b.stateStore().Inbox(time.Time{}, true)
			for _, i := range items {
				if i.ID == target {
					return "Acknowledge " + i.ID + " · " + i.Summary
				}
			}
		}
		if args.All {
			target = "all"
		}
		if args.Project != "" {
			target = args.Project
		}
		return name + " " + target
	}
	var builtin struct {
		Command string `json:"command"`
		Path    string `json:"file_path"`
		Content string `json:"content"`
		Old     string `json:"old_string"`
		New     string `json:"new_string"`
	}
	_ = json.Unmarshal(in.Input, &builtin)
	title := in.Tool
	if builtin.Command != "" {
		title += " · " + builtin.Command
	}
	if builtin.Path != "" {
		title += " · " + builtin.Path
	}
	if in.Tool == "Write" {
		title += fmt.Sprintf(" · %d bytes", len(builtin.Content))
	}
	if in.Tool == "Edit" {
		title += fmt.Sprintf(" · %d → %d bytes", len(builtin.Old), len(builtin.New))
	}
	return strings.Join(strings.Fields(title), " ")
}
func autoAllowed(tool string, input json.RawMessage, dir string, project bool) bool {
	switch tool {
	case "mcp__driga__ack_inbox", "mcp__driga__jump", "mcp__driga__summarize":
		return true
	case "Edit", "Write":
		var in struct {
			Path string `json:"file_path"`
		}
		if json.Unmarshal(input, &in) != nil || in.Path == "" || !project {
			return false
		}
		// Check the resolved path too: a link inside the project may point at .git.
		rel, ok := insideDirectory(dir, in.Path)
		given := strings.TrimPrefix(in.Path, strings.TrimRight(dir, `/\`)+string(filepath.Separator))
		given = strings.TrimPrefix(given, filepath.VolumeName(given))
		return ok && !configPath(given) && !configPath(rel)
	}
	return false
}

// autoProject excludes broad working directories even if they contain a repo.
func autoProject(dir, dataDir string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || dataDir == "" || dir == "" {
		return false
	}
	for _, protected := range []string{home, dataDir} {
		if _, inside := insideDirectory(dir, gitscan.NormalizePath(protected)); inside {
			return false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	// A caller's Git overrides must not make an unrelated directory a project.
	cmd.Env = []string{}
	for _, env := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(env), "GIT_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// Hidden components and agent instructions always ask, using Win32's spelling
// rules on every platform so trailing dots/spaces and streams cannot bypass it.
func configPath(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.Contains(part, ":") {
			return true
		}
		part = strings.ToLower(strings.TrimRight(part, ". "))
		if strings.HasPrefix(part, ".") || part == "copilot-instructions.md" {
			return true
		}
		for _, prefix := range []string{"claude", "agents", "gemini"} {
			if strings.HasPrefix(part, prefix) && strings.HasSuffix(part, ".md") {
				return true
			}
		}
	}
	return false
}

// insideDirectory resolves path against dir, following links, and returns
// the result relative to dir when it stays inside.
func insideDirectory(dir, path string) (string, bool) {
	if strings.HasPrefix(path, "~") {
		return "", false
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", false
	}
	current := root
	if len(path) > 0 && os.IsPathSeparator(path[0]) && !filepath.IsAbs(path) {
		return "", false
	}
	if filepath.IsAbs(path) {
		volume := filepath.VolumeName(path)
		current = volume + string(filepath.Separator)
		path = strings.TrimLeft(path[len(volume):], string(filepath.Separator))
	} else if filepath.VolumeName(path) != "" {
		return "", false
	}
	// Walk before cleaning: link/../file follows the link before applying '..'.
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r <= 127 && os.IsPathSeparator(uint8(r)) }) {
		if part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			current, err = filepath.EvalSymlinks(current)
			if err != nil {
				return "", false
			}
		}
	}
	rel, err := filepath.Rel(root, current)
	return rel, err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (b *Broker) remoteTarget(in Approval) bool {
	if !strings.HasPrefix(in.Tool, "mcp__driga__") {
		return false
	}
	var args struct {
		ID, Machine string
		SessionID   int64 `json:"session_id"`
	}
	if json.Unmarshal(in.Input, &args) != nil {
		return false
	}
	st := b.stateStore()
	if args.Machine != "" && args.Machine != "local" && args.Machine != st.MachineID() {
		return true
	}
	if args.SessionID > 0 && (in.Tool == "mcp__driga__kill" || in.Tool == "mcp__driga__jump") {
		if s, err := st.SessionScoped(args.SessionID, store.Scope{}); err == nil && s.MachineID != st.MachineID() {
			return true
		}
	}
	if args.ID == "" {
		return false
	}
	switch in.Tool {
	case "mcp__driga__ack_inbox":
		items, _ := st.InboxScoped(time.Time{}, true, store.Scope{})
		remote := false
		for _, i := range items {
			if i.ID == args.ID {
				if i.MachineID == st.MachineID() {
					return false
				}
				remote = true
			}
		}
		if remote {
			return true
		}
	case "mcp__driga__kill":
		items, _ := st.OrphansScoped(store.Scope{})
		remote := false
		for _, i := range items {
			if i.ID == args.ID {
				if i.MachineID == st.MachineID() {
					return false
				}
				remote = true
			}
		}
		if remote {
			return true
		}
	}
	return false
}

func canonicalInput(raw json.RawMessage) (string, error) {
	var v any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		return "", err
	}
	data, err := json.Marshal(v)
	return string(data), err
}
