//go:build !windows

package cli

import (
	"bytes"
	"encoding/json"
	"github.com/labstack/driga/internal/spool"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestClaudePermissionTranscriptSpecialFiles(t *testing.T) {
	for _, kind := range []string{"directory", "FIFO", "symlink FIFO"} {
		t.Run(kind, func(t *testing.T) {
			dir := hookTestEnv(t)
			path := t.TempDir()
			if kind != "directory" {
				path = filepath.Join(path, "fifo")
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink FIFO" {
					link := path + ".link"
					if err := os.Symlink(path, link); err != nil {
						t.Fatal(err)
					}
					path = link
				}
			}
			payload, _ := json.Marshal(map[string]string{"session_id": "s", "notification_type": "permission_prompt", "transcript_path": path})
			done := make(chan error, 1)
			go func() {
				done <- (HookCmd{Adapter: "claude", Event: "Notification"}).Run(&Globals{Stdin: bytes.NewReader(payload)})
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("transcript blocked hook")
			}
			events, _, err := spool.Drain(dir, nil)
			if err != nil || len(events) != 1 || events[0].Tool != "" {
				t.Fatal(events, err)
			}
		})
	}
}
