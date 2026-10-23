package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
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
		action, err := chat.Claim(ctx, os.Getenv("DRIGA_CHAT_SOCKET"), approval)
		if err != nil {
			return "", err
		}
		var out bytes.Buffer
		g := *base
		g.Machine = "local"
		g.Stdout = &out
		g.Stdin = nil
		g.JSON = false
		g.Watch = 0
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
		return (CleanupCmd{ID: in.ID, Yes: true}).Run(g)
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
			return killChatSession(ctx, g, in.SessionID, in.Force)
		}
		return (OrphansKillCmd{ID: in.ID, Force: in.Force, approved: true}).run(ctx, g)
	case "jump":
		if in.SessionID <= 0 {
			return errors.New("session_id must be positive")
		}
		st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
		if err != nil {
			return err
		}
		defer st.Close()
		sessions, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			return err
		}
		items, err := st.Inbox(time.Time{}, true)
		if err != nil {
			return err
		}
		for _, s := range sessions {
			if s.ID == in.SessionID {
				procs, err := st.Procs()
				if err != nil {
					return err
				}
				tty := ""
				for _, p := range procs {
					if p.PID == s.PID && derive.SameStart(p.StartTime, s.ProcStart) {
						tty = p.TTY
					}
				}
				j := inbox.ResolveJump(s, tty, inbox.ReadPanes(ctx, newInboxRunner()))
				for _, item := range items {
					if item.SessionID == s.ID && item.Jump != nil {
						j = item.Jump
						break
					}
				}
				return runChatJump(ctx, g, j)
			}
		}
		return missingAgent(in.SessionID)
	}
	return errors.New("unknown Chat action")
}
func runChatJump(ctx context.Context, g *Globals, j *model.InboxJump) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	opts := inbox.JumpOptions{DrigaPath: exe, Tmux: os.Getenv("TMUX"), Terminal: cfg.Inbox.JumpTerminal}
	notice, err := inbox.RunJump(ctx, newInboxRunner(), j, opts)
	if err != nil {
		return err
	}
	if command := inbox.JumpCommand(j); command != "" {
		fmt.Fprintln(g.Stdout, command)
	}
	if notice != "" {
		fmt.Fprintln(g.Stdout, notice)
	}
	return nil
}
func killChatSession(ctx context.Context, g *Globals, id int64, force bool) error {
	st, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
	if err != nil {
		return err
	}
	defer st.Close()
	sessions, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return err
	}
	procs, err := st.Procs()
	if err != nil {
		return err
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	for _, s := range sessions {
		if s.ID == id {
			if s.PID <= 1 || s.ProcStart.IsZero() || s.EndedAt != nil {
				return errors.New("session process identity unavailable")
			}
			idx := derive.NewProcIndex(procs)
			root, ok := idx.Get(s.PID)
			if !ok || !derive.SameStart(root.StartTime, s.ProcStart) {
				return errors.New("session process changed or exited")
			}
			o := model.Orphan{ID: fmt.Sprintf("session:%d", id), RootPID: s.PID, StartTime: root.StartTime, Young: time.Since(root.StartTime) < cfg.OrphanMinAge, ProcessStarts: map[int32]time.Time{s.PID: root.StartTime}, PIDs: []int32{s.PID}}
			for _, p := range idx.Descendants(s.PID) {
				o.PIDs = append(o.PIDs, p.PID)
				o.ProcessStarts[p.PID] = p.StartTime
			}
			return (OrphansKillCmd{Force: force, approved: true}).killCandidates(ctx, g, cfg, procs, []model.Orphan{o})
		}
	}
	return missingAgent(id)
}
