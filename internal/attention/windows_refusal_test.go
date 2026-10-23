package attention

import (
	"context"
	"strings"
	"testing"
)

func TestWindowsNotifierUsesNativeBackend(t *testing.T) {
	r := runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("Windows notification launched a command")
		return nil, nil
	})
	var payload string
	d := DesktopNotifier{OS: "windows", Runner: r, Toast: func(_ context.Context, xml string) error { payload = xml; return nil }}
	if err := d.Notify(context.Background(), Notification{Title: "Needs you", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, "Needs you") {
		t.Fatal(payload)
	}
	d.Toast = nil
	if err := d.Notify(context.Background(), Notification{}); err == nil || !strings.Contains(err.Error(), "notifications unavailable") {
		t.Fatal(err)
	}
}
