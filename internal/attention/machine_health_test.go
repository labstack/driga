package attention

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
)

func TestMachineAlertRepeatAndBatch(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	c := config.Default().Inbox
	seen, last := map[string]time.Time{}, map[string]time.Time{}
	items := []model.InboxItem{{ID: "disk:1", MachineID: "local", Kind: "low_disk", TS: at, Summary: "Low disk; inspect known cleanup candidates"}, {ID: "pressure:1", MachineID: "local", Kind: "memory_pressure", TS: at, Summary: "Critical memory; inspect largest apps"}}
	ns := PlanAlerts(items, nil, c, "titles", at, seen, last)
	if len(ns) != 1 || !strings.Contains(ns[0].Body, "largest apps") || !strings.Contains(ns[0].Body, "cleanup") || ns[0].ItemID == "" {
		t.Fatal(ns)
	}
	if ns = PlanAlerts(items, nil, c, "titles", at.Add(time.Minute), seen, last); len(ns) != 0 {
		t.Fatal(ns)
	}
	if ns = PlanAlerts(items, nil, c, "titles", at.Add(c.AlertRepeat), seen, last); len(ns) != 1 {
		t.Fatal(ns)
	}
	c.Alerts = "off"
	if ns = PlanAlerts(items, nil, c, "titles", at.Add(2*c.AlertRepeat), seen, last); len(ns) != 0 {
		t.Fatal(ns)
	}
}

func TestMachineJumpPlansSafeTerminal(t *testing.T) {
	bin := "/opt/my app's/driga"
	cmds, err := JumpCommands(&model.InboxJump{Kind: "machine"}, JumpOptions{OS: "darwin", DrigaPath: bin})
	if err != nil || len(cmds) != 1 || cmds[0][0] != "osascript" || cmds[0][3] != Quote(bin)+" top --tab machine" {
		t.Fatalf("%q %v", cmds, err)
	}
	cmds, err = JumpCommands(&model.InboxJump{Kind: "machine"}, JumpOptions{OS: "linux", DrigaPath: bin})
	if err != nil || !reflect.DeepEqual(cmds, [][]string{{"x-terminal-emulator", "-e", bin, "top", "--tab", "machine"}}) {
		t.Fatalf("%q %v", cmds, err)
	}
}
