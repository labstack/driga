package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestDoctorCollectorBudgetUsesLockOwner(t *testing.T) {
	d, st := doctorFixture(t)
	start := d.Now.Add(-time.Hour)
	if err := os.WriteFile(filepath.Join(d.DataDir, "driga.lock"), []byte(`{"pid":42,"kind":"service","executable":"/opt/driga","version":"test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	d.ProcessAlive = func(pid int32, at time.Time) bool { return pid == 42 && at.Equal(start) }
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{
			{PID: 42, StartTime: start, Cmdline: "/opt/driga collect", Argv: []string{"/opt/driga", "collect"}, RSS: 20_000_000},
			{PID: 43, StartTime: start, Cmdline: "/debug/driga collect", Argv: []string{"/debug/driga", "collect"}, RSS: 90_000_000},
		})
	}); err != nil {
		t.Fatal(err)
	}
	checks := Doctor(context.Background(), d)
	c := requireCheck(t, checks, "collector_rss", "ok", "")
	if c.Detail != "20.0 MB" || c.Fix != "" {
		t.Fatal(c)
	}
	requireCheck(t, checks, "other_collectors", "info", "")
	d.ProcessAlive = func(int32, time.Time) bool { return false }
	c = requireCheck(t, Doctor(context.Background(), d), "collector_rss", "warn", "")
	if strings.Contains(c.Detail, "20.0 MB") {
		t.Fatal("reused/dead PID counted", c)
	}
	// A renamed binary (a development build) is still the lock owner.
	d.ProcessAlive = func(pid int32, at time.Time) bool { return pid == 42 && at.Equal(start) }
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{{PID: 42, StartTime: start, Exe: "/opt/driga-dev", Cmdline: "/opt/driga-dev collect", Argv: []string{"/opt/driga-dev", "collect"}, RSS: 20_000_000}})
	}); err != nil {
		t.Fatal(err)
	}
	if c = requireCheck(t, Doctor(context.Background(), d), "collector_rss", "ok", ""); c.Detail != "20.0 MB" {
		t.Fatal(c)
	}
}
