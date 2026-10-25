package actions

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/self"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func JumpSession(ctx context.Context, g Environment, runner attention.Runner, id int64) error {
	if id <= 0 {
		return InvalidUsage("session_id must be positive")
	}
	if err := g.Confirm(fmt.Sprintf("Jump to session %d?", id)); err != nil {
		return err
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
		if s.ID == id {
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
			j := attention.ResolveJump(s, tty, attention.ReadPanes(ctx, runner))
			for _, item := range items {
				if item.SessionID == s.ID && item.Jump != nil {
					j = item.Jump
					break
				}
			}
			return Jump(ctx, g, runner, j)
		}
	}
	return fmt.Errorf("agent %d not found", id)
}
func Jump(ctx context.Context, g Environment, runner attention.Runner, j *model.InboxJump) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	exe, err := self.Executable()
	if err != nil {
		return err
	}
	opts := attention.JumpOptions{DrigaPath: exe, Tmux: os.Getenv("TMUX"), Terminal: cfg.Inbox.JumpTerminal}
	notice, err := attention.RunJump(ctx, runner, j, opts)
	if err != nil {
		return err
	}
	if command := attention.JumpCommand(j); command != "" {
		if _, err := fmt.Fprintln(g.Stdout, command); err != nil {
			return err
		}
	}
	if notice != "" {
		if _, err := fmt.Fprintln(g.Stdout, notice); err != nil {
			return err
		}
	}
	return nil
}
func KillSession(ctx context.Context, g Environment, id int64, force bool) error {
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
			return (KillOptions{Force: force}).Kill(ctx, g, cfg, procs, []model.Orphan{o})
		}
	}
	return fmt.Errorf("agent %d not found", id)
}
