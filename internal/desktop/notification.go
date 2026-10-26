package desktop

import (
	"encoding/json"
	"github.com/egoist/mygo"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
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
		snap, err := s.source.LoadScope(s.ctx, store.Scope{})
		name, arg := "view", any("inbox")
		if err == nil {
			name, arg = notificationCommand(target, snap.Inbox.Rows)
		}
		mygo.RunOnMain(func() { s.host.Command(name, arg) })
	}()
}
