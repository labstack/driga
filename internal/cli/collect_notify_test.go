package cli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/config"
)

func TestCollectNotifyFDFlag(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want int
	}{
		{[]string{"collect"}, -1},
		{[]string{"collect", "--notify-fd", "3"}, 3},
		{[]string{"collect", "--notify-fd=0"}, 0},
	} {
		root, _, err := parseCommand(t, tt.args...)
		if err != nil {
			t.Fatal(err)
		}
		fd := root.Collect.NotifyFD
		if tt.want == -1 {
			if fd != nil {
				t.Fatal("notify fd set by default")
			}
		} else if fd == nil || *fd != tt.want {
			t.Fatalf("notify fd = %v, want %d", fd, tt.want)
		}
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"collect", "--help"}, "test", strings.NewReader(""), &out, &diagnostic); code != 0 {
		t.Fatalf("help: code=%d, %s", code, &diagnostic)
	}
	if strings.Contains(out.String(), "--notify-fd") {
		t.Fatal("notify fd flag appears in help")
	}
	fd := -1
	if err := (CollectCmd{NotifyFD: &fd}).Run(nil); err == nil {
		t.Fatal("negative notify fd accepted")
	}
}

func TestDepsUsePipeNotifier(t *testing.T) {
	var buf bytes.Buffer
	notifier := &attention.PipeNotifier{W: &buf}
	d, err := newDeps(config.Default(), notifier)
	if err != nil {
		t.Fatal(err)
	}
	if d.Notifier != notifier {
		t.Fatalf("notifier = %T, want supplied pipe", d.Notifier)
	}
	want := attention.Notification{Kind: "waiting", Title: "Needs you", ItemID: "item", MachineID: "machine"}
	if err := d.Notifier.Notify(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	var got []attention.Notification
	if err := attention.ReadNotifications(&buf, func(n attention.Notification) { got = append(got, n) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []attention.Notification{want}) {
		t.Fatalf("forwarded notifications = %+v", got)
	}
}
