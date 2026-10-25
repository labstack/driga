package desktop

import (
	"context"
	"flag"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/attention"
)

// Not a DRIGA_* name: the child takes those from the app only.
const helperEnv = "DESKTOP_TEST_COLLECTOR"

// TestMain lets the test binary stand in for `driga collect`: it reports its
// arguments and environment as a notification, then runs until its parent
// closes the lifeline.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "" {
		os.Exit(m.Run())
	}
	args := os.Args[1:]
	fs := flag.NewFlagSet("collect", flag.ExitOnError)
	owner := fs.String("owner", "", "")
	notifyFD := fs.Int("notify-fd", -1, "")
	parentFD := fs.Int("parent-fd", -1, "")
	if len(args) == 0 || args[0] != "collect" || fs.Parse(args[1:]) != nil || *owner != "app" {
		os.Exit(2)
	}
	notifier := &attention.PipeNotifier{W: os.NewFile(uintptr(*notifyFD), "notify")}
	n := attention.Notification{Kind: "helper", Title: strings.Join(args, " "), Body: strings.Join(os.Environ(), "\n")}
	if err := notifier.Notify(context.Background(), n); err != nil {
		os.Exit(3)
	}
	_, _ = io.Copy(io.Discard, os.NewFile(uintptr(*parentFD), "parent"))
	os.Exit(0)
}

func TestStartCollectorRunsUntilStopped(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "hunter2")
	child, err := StartCollector(context.Background(), os.Args[0], map[string]string{helperEnv: "1"})
	if err != nil {
		t.Fatal(err)
	}
	notes := make(chan attention.Notification, 4)
	go func() {
		_ = attention.ReadNotifications(child.Notifications(), func(n attention.Notification) { notes <- n })
		close(notes)
	}()
	var n attention.Notification
	select {
	case n = <-notes:
	case <-time.After(10 * time.Second):
		t.Fatal("no notification from the child")
	}
	if runtime.GOOS != "windows" && n.Title != "collect --owner=app --notify-fd=3 --parent-fd=4" {
		t.Fatalf("child args = %q", n.Title)
	}
	env := strings.Split(n.Body, "\n")
	for _, entry := range env {
		if entry == "SECRET_TOKEN=hunter2" && runtime.GOOS != "windows" {
			t.Fatal("child inherited the app's whole environment")
		}
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	select {
	case err := <-exited:
		t.Fatalf("child exited before stop: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := child.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("child exit = %v, want clean exit from the lifeline", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not exit")
	}
	select {
	case _, ok := <-notes:
		if ok {
			t.Fatal("unexpected second notification")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("notification stream did not end with the child")
	}
}
