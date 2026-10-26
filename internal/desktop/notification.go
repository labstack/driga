package desktop

import (
	"context"
	"encoding/json"
	"github.com/egoist/mygo"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
	"time"
)

func notificationFromArgs(args []string) *model.NotificationTarget {
	for i, arg := range args {
		if arg == "--notification" && i+1 < len(args) {
			var target model.NotificationTarget
			if json.Unmarshal([]byte(args[i+1]), &target) == nil {
				return &target
			}
		}
	}
	return nil
}

func notificationCommand(target model.NotificationTarget, rows []view.InboxRow) (string, any) {
	if target.Kind == "machine_health" || target.Kind == "low_disk" || target.Kind == "memory_pressure" {
		return "view", "fleet"
	}
	for _, row := range rows {
		if target.ItemID != "" && row.ID == target.ItemID && row.MachineID == target.MachineID {
			if row.SessionID > 0 {
				return "openSession", map[string]any{"session_id": row.SessionID, "machine_id": row.MachineID}
			}
			if row.Kind == "low_disk" || row.Kind == "memory_pressure" || row.Jump != nil && row.Jump.Kind == "machine" {
				return "view", "fleet"
			}
		}
	}
	return "view", "inbox"
}

func (s *shell) openNotification(target model.NotificationTarget) {
	go func() {
		snap, err := notificationSnapshot(s.ctx, s.source)
		name, arg := "view", any("inbox")
		if err == nil {
			name, arg = notificationCommand(target, snap.Inbox.Rows)
		}
		mygo.RunOnMain(func() { s.host.Command(name, arg) })
	}()
}

func notificationSnapshot(parent context.Context, source interface {
	LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error)
}) (viewer.Snapshot, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	return source.LoadScope(ctx, store.Scope{})
}
