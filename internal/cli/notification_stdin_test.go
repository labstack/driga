package cli

import (
	"github.com/labstack/driga/internal/desktop"
	"os"
	"strings"
	"testing"
)

func TestAppNotificationStdinPreservesTargetAndIPC(t *testing.T) {
	readEnv(t)
	previous := runDesktop
	defer func() { runDesktop = previous }()
	called := false
	original := append([]string(nil), os.Args...)
	runDesktop = func(o desktop.Options) error {
		called = true
		if o.Notification == nil || o.Notification.ItemID != "private-item" || o.Notification.MachineID != "peer" {
			t.Fatalf("target=%+v", o.Notification)
		}
		if !strings.Contains(strings.Join(os.Args, " "), "private-item") {
			t.Fatal("IPC target missing")
		}
		return nil
	}
	if !appLaunch(func(string) string { return "" }, false, []string{"app", "--notification-stdin"}) {
		t.Fatal("stdin launch missed")
	}
	if err := (AppCmd{NotificationStdin: true}).Run(&Globals{Stdin: strings.NewReader(`{"item_id":"private-item","machine_id":"peer"}`)}); err != nil || !called {
		t.Fatalf("%v called%v", err, called)
	}
	if strings.Join(os.Args, "\x00") != strings.Join(original, "\x00") {
		t.Fatal("invocation values not restored")
	}
}
