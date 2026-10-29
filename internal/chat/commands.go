package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/driga/internal/command"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/secfile"
)

func clonePrepared(p command.Prepared) command.Prepared {
	p.Parsed.Request.Argv = append([]string(nil), p.Parsed.Request.Argv...)
	p.Parsed.Commands = append([][]string(nil), p.Parsed.Commands...)
	for i := range p.Parsed.Commands {
		p.Parsed.Commands[i] = append([]string(nil), p.Parsed.Commands[i]...)
	}
	p.Scope.Denied = append([]string(nil), p.Scope.Denied...)
	p.Policy.Commands = append([]command.Executable(nil), p.Policy.Commands...)
	for i := range p.Policy.Commands {
		p.Policy.Commands[i].Argv = append([]string(nil), p.Policy.Commands[i].Argv...)
	}
	p.BackendExecutable.Argv = append([]string(nil), p.BackendExecutable.Argv...)
	if p.ProcessSnapshot != nil {
		s := *p.ProcessSnapshot
		s.Targets = append([]command.ProcessTarget(nil), s.Targets...)
		s.Protected = append([]command.ProcessTarget(nil), s.Protected...)
		for i := range s.Targets {
			s.Targets[i].Proc.Argv = append([]string(nil), s.Targets[i].Proc.Argv...)
		}
		for i := range s.Protected {
			s.Protected[i].Proc.Argv = append([]string(nil), s.Protected[i].Proc.Argv...)
		}
		p.ProcessSnapshot = &s
	}
	return p
}
func copyAction(a Action) Action {
	if a.Delegated != nil {
		cp := copyPrompt(*a.Delegated)
		a.Delegated = &cp
	}
	a.Input = append(json.RawMessage(nil), a.Input...)
	if a.Command != nil {
		p := copyCommandCard(*a.Command)
		a.Command = &p
	}
	return a
}
func commandAutomatic(mode Mode, p command.Prepared) bool {
	return (mode == Auto || mode == FullAuto) && p.Policy.Class == command.Read || mode == FullAuto && p.Policy.Class == command.Other && p.Sandbox.Available
}
func decodeCommand(raw json.RawMessage) (command.Request, error) {
	var req command.Request
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		return req, err
	}
	if d.Decode(new(any)) != io.EOF {
		return req, errors.New("invalid command request")
	}
	return req, nil
}
func (b *Broker) prepareCommand(ctx context.Context, in Approval) (command.Prepared, error) {
	empty := command.Prepared{}
	if in.Tool != "mcp__driga__run_command" || len(in.Input) > MaxText {
		return empty, errors.New("invalid command gateway request")
	}
	req, err := decodeCommand(in.Input)
	if err != nil {
		return empty, err
	}
	b.mu.Lock()
	c, err := b.approvalConversation(in)
	if err != nil {
		b.mu.Unlock()
		return empty, err
	}
	if c.remote {
		b.mu.Unlock()
		return empty, errors.New("Actions on another machine are unavailable; run driga there")
	}
	if c.commandScratch == "" {
		c.commandScratch, err = os.MkdirTemp("", "driga-command-")
		if err == nil {
			err = secfile.Protect(c.commandScratch)
		}
	}
	scope := command.Scope{Project: c.commandProject, Scratch: c.commandScratch, Denied: []string{b.opts.DataDir}}
	if b.socket != "" {
		scope.Denied = append(scope.Denied, filepath.Dir(b.socket))
	}
	b.mu.Unlock()
	if err != nil {
		return empty, err
	}
	// Relative cwd is relative to the selected project, never the service cwd.
	if req.Cwd != "" && !filepath.IsAbs(req.Cwd) {
		root := scope.Project
		if root == "" {
			root = scope.Scratch
		}
		req.Cwd = filepath.Join(root, req.Cwd)
	}
	p, err := command.Prepare(ctx, req, scope)
	if err != nil {
		return empty, err
	}
	if p.Policy.Class == command.Never {
		return empty, fmt.Errorf("command refused: %s", p.Policy.Reason)
	}
	// A broad project that contains protected configuration is not a writable
	// command scope. Denials inside the sandbox are an additional boundary.
	for _, root := range []string{p.Scope.Project, p.Scope.Scratch} {
		if root == "" {
			continue
		}
		for _, secret := range p.Scope.Denied {
			if _, ok := insideDirectory(root, secret); ok {
				return empty, errors.New("selected scope overlaps protected configuration")
			}
		}
	}
	if p.Policy.Class == command.Process {
		protected, e := b.commandProtected(ctx)
		if e != nil {
			return empty, e
		}
		return command.PrepareProcessTargets(ctx, b.opts.CommandPlatform, p, protected)
	}
	return p, nil
}
func (b *Broker) commandProtected(ctx context.Context) ([]model.Proc, error) {
	ps, err := b.opts.CommandPlatform.Procs(ctx)
	if err != nil {
		return nil, err
	}
	self := -1
	for i, p := range ps {
		if p.PID == int32(os.Getpid()) {
			self = i
			break
		}
	}
	if self < 0 || !kill.Owned(ps[self]) || ps[self].StartTime.IsZero() || ps[self].Partial || ps[self].Exe == "" || len(ps[self].Argv) == 0 || ps[self].Cmdline == "" {
		return nil, errors.New("cannot establish live broker protection")
	}
	// The broker root protects even agent children whose session ID has not yet
	// arrived. The live graph also recognizes app/service Driga executables.
	return []model.Proc{ps[self]}, nil
}
func (b *Broker) approveCommand(ctx context.Context, in Approval, p command.Prepared) (string, error) {
	if p.Policy.Class == command.Never {
		return "", fmt.Errorf("command refused: %s", p.Policy.Reason)
	}
	if in.ToolUseID == "" {
		return "", errors.New("command request identity required")
	}
	perm, err := b.approve(ctx, in, &p)
	if err != nil {
		return "", err
	}
	if perm.Behavior != "allow" {
		return "", fmt.Errorf("command denied: %s", perm.Message)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.approvalConversation(in)
	if err != nil {
		return "", err
	}
	for _, a := range c.actions {
		if a.token == in.Token && a.toolUseID == in.ToolUseID && a.prepared != nil {
			return a.ID, nil
		}
	}
	return "", errors.New("command action missing")
}
func (b *Broker) RunCommand(ctx context.Context, in Approval) (command.Result, error) {
	b.mu.Lock()
	_, err := b.approvalConversation(in)
	if err == nil {
		b.wg.Add(1)
	}
	b.mu.Unlock()
	if err != nil {
		return command.Result{}, err
	}
	defer b.wg.Done()
	p, err := b.prepareCommand(ctx, in)
	if err != nil {
		return command.Result{}, err
	}
	id, err := b.approveCommand(ctx, in, p)
	if err != nil {
		return command.Result{}, err
	}
	result, err := b.executeCommand(ctx, in, id, command.Digest(p))
	if err != nil || len(result.Processes.Survivors) == 0 {
		return result, err
	}
	// No fresh selector is ever evaluated for force. A new card binds only the
	// surviving identities from this approved TERM execution.
	force, err := command.PrepareForce(p, result.Processes.Survivors)
	if err != nil {
		return result, err
	}
	in.ToolUseID = uuid.NewString()
	id, err = b.approveCommand(ctx, in, force)
	if err != nil {
		return result, err
	}
	forced, err := b.executeCommand(ctx, in, id, command.Digest(force))
	forced.Processes.Stopped = append(result.Processes.Stopped, forced.Processes.Stopped...)
	forced.Processes.Vanished = append(result.Processes.Vanished, forced.Processes.Vanished...)
	forced.Processes.Skipped = append(result.Processes.Skipped, forced.Processes.Skipped...)
	if forced.Processes.Reasons == nil {
		forced.Processes.Reasons = make(map[int32]string)
	}
	for pid, reason := range result.Processes.Reasons {
		if _, ok := forced.Processes.Reasons[pid]; !ok {
			forced.Processes.Reasons[pid] = reason
		}
	}
	return forced, err
}
func (b *Broker) executeCommand(ctx context.Context, in Approval, actionID string, digest [32]byte) (command.Result, error) {
	b.mu.Lock()
	c, err := b.approvalConversation(in)
	if err != nil {
		b.mu.Unlock()
		return command.Result{}, err
	}
	var a *pendingAction
	for _, candidate := range c.actions {
		if candidate.ID == actionID {
			a = candidate
			break
		}
	}
	want, e := canonicalInput(in.Input)
	if a == nil || a.prepared == nil || a.token != in.Token || a.Tool != in.Tool || a.Status != "allowed" || a.consumed || a.digest != digest || command.Digest(*a.prepared) != digest || e != nil {
		b.mu.Unlock()
		return command.Result{}, errors.New("command authority invalid or consumed")
	}
	got, _ := canonicalInput(a.Input)
	if got != want || !sameCommandRoot(a.prepared.Scope.Project, c.commandProject) || !sameCommandRoot(a.prepared.Scope.Scratch, c.commandScratch) || ctx.Err() != nil {
		b.mu.Unlock()
		return command.Result{}, errors.New("command request or scope changed")
	}
	if a.DecidedBy == grantAttribution {
		g, ok := b.grants[a.GrantID]
		if !ok || !b.now().Before(g.ExpiresAt) || g.Conversation != c.row.ID || !command.MatchPattern(g.Pattern, *a.prepared) {
			b.mu.Unlock()
			return command.Result{}, errors.New("grant expired or revoked")
		}
	}
	a.consumed = true
	a.claimed = true
	run, cancel := context.WithTimeout(ctx, time.Duration(a.prepared.Parsed.Request.TimeoutSeconds)*time.Second)
	a.execCancel = cancel
	p := clonePrepared(*a.prepared)
	b.mu.Unlock()
	defer cancel()
	output := newCommandOutput(b.st, a.ID, b.opts.Snippets)
	var result command.Result
	if p.Policy.Class == command.Process {
		protected, e := b.commandProtected(run)
		if e != nil {
			err = e
		} else {
			var targets []model.Proc
			for _, t := range p.ProcessSnapshot.Targets {
				v := t.Proc
				v.OwnerUID = t.OwnerUID
				v.OwnerSID = t.OwnerSID
				v.OwnerKnown = t.OwnerKnown
				targets = append(targets, v)
			}
			result.Processes, err = kill.Snapshot(run, b.opts.CommandPlatform, targets, protected, p.ProcessSnapshot.Force, time.Second)
			result.TimedOut = errors.Is(run.Err(), context.DeadlineExceeded)
		}
	} else {
		result, err = command.Run(run, p, &cancelOutput{output.Writer("stdout"), cancel}, &cancelOutput{output.Writer("stderr"), cancel})
	}
	summary, outputErr := output.Finish(context.Background())
	result.Output = summary
	err = errors.Join(err, outputErr)
	b.mu.Lock()
	defer b.mu.Unlock()
	a.execCancel = nil
	a.Status = "done"
	if err != nil || result.ExitCode != 0 {
		a.Status = "failed"
	}
	if a.Command != nil {
		for _, group := range []struct {
			in  []model.Proc
			out *[]ProcessRow
		}{{result.Processes.Stopped, &a.Command.Stopped}, {result.Processes.Vanished, &a.Command.Vanished}, {result.Processes.Skipped, &a.Command.Skipped}, {result.Processes.Survivors, &a.Command.Survivors}} {
			for _, v := range group.in {
				r := processRow(v)
				r.Reason = result.Processes.Reasons[v.PID]
				*group.out = append(*group.out, r)
			}
		}
	}
	a.Result = summary
	if p.Policy.Class == command.Process {
		a.Result = fmt.Sprintf("Stopped %d, vanished %d, skipped %d, survivors %d", len(result.Processes.Stopped), len(result.Processes.Vanished), len(result.Processes.Skipped), len(result.Processes.Survivors))
	}
	if err != nil {
		a.Result += " · " + err.Error()
	}
	err = errors.Join(err, b.actionEvent(c, a))
	return result, err
}

type cancelOutput struct {
	io.Writer
	cancel context.CancelFunc
}

func (w *cancelOutput) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err != nil {
		w.cancel()
	}
	return n, err
}
func (b *Broker) cancelCommands(c *conversation) {
	for _, a := range c.actions {
		if a.execCancel != nil {
			a.execCancel()
		}
	}
}

// RunCommand is a turn-authenticated, one-shot bridge. A failed transport is
// never replayed because execution may already have begun.
func RunCommand(ctx context.Context, path string, in Approval) (command.Result, error) {
	c, err := dialPath(ctx, path)
	if err != nil {
		return command.Result{}, err
	}
	defer c.Close()
	var out command.Result
	err = c.callOnce(ctx, "POST", "/run-command", in, &out, false)
	return out, err
}

func sameCommandRoot(prepared, selected string) bool {
	if prepared == "" || selected == "" {
		return prepared == selected
	}
	resolved, err := filepath.EvalSymlinks(selected)
	return err == nil && resolved == prepared
}
