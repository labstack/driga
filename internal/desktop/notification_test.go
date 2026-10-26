package desktop

import (
	"github.com/labstack/driga/internal/model"
	"reflect"
	"testing"
)

func TestNotificationDestination(t *testing.T) {
	rows := inboxRows()
	rows[2].Kind = "low_disk"
	for _, tc := range []struct {
		target model.NotificationTarget
		name   string
		arg    any
	}{
		{model.NotificationTarget{ItemID: "w1", MachineID: "local"}, "openSession", map[string]any{"session_id": int64(42), "machine_id": "local"}},
		{model.NotificationTarget{ItemID: "w1", MachineID: "peer"}, "openSession", map[string]any{"session_id": int64(7), "machine_id": "peer"}},
		{model.NotificationTarget{ItemID: "health", MachineID: "local"}, "view", "fleet"},
		{model.NotificationTarget{}, "view", "inbox"},
		{model.NotificationTarget{Kind: "machine_health", MachineID: "local"}, "view", "fleet"},
		{model.NotificationTarget{ItemID: "gone", MachineID: "local"}, "view", "inbox"},
	} {
		name, arg := notificationCommand(tc.target, rows)
		if name != tc.name || !reflect.DeepEqual(arg, tc.arg) {
			t.Fatalf("%+v: %s %+v", tc.target, name, arg)
		}
	}
	target := notificationFromArgs([]string{"/app/driga", "app", "--notification", `{"item_id":"w1","machine_id":"peer"}`})
	if target == nil || target.MachineID != "peer" {
		t.Fatal(target)
	}
	if notificationFromArgs([]string{"app", "--notification", "bad"}) != nil {
		t.Fatal("accepted invalid target")
	}
}
