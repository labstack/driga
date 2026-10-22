package collector

import (
	"context"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/store"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestChatListenFailureKeepsCollectingAndServing(t *testing.T) {
	d, _, _, _ := setup(t)
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = os.TempDir()
	}
	root, err := os.MkdirTemp(base, "dc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	path := chat.SocketPath(root)
	if err := fleet.EnsureSocketDir(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	d.Cfg.Fleet.Serve = true
	d.Cfg.Fleet.Listen = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, root) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("collector did not stop")
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := store.Open(filepath.Join(root, "driga.db"), store.ReadOnly)
		if err == nil {
			ss, _ := st.Sessions(store.SessionFilter{})
			sources, _ := st.Sources()
			st.Close()
			warned := false
			for _, source := range sources {
				if source.Name == "chat" && source.LastError != "" {
					warned = true
				}
			}
			if len(ss) > 0 && warned {
				if _, err := fleet.Control(ctx, root, fleet.ControlRequest{Op: "status"}); err == nil {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no collection, serving, and chat warning after listener failure")
}
