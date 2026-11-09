package actions

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/view"
)

func TestExecutePersistsCancelledOutcome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DRIGA_DATA_DIR", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := (KillOptions{}).Execute(ctx, Environment{Stdout: io.Discard}, config.Config{}, nil, []model.Orphan{{ID: "tree-1", MachineID: "host-1", RootPID: 42}})
	if err == nil || len(report.Outcomes) != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	history, err := reclaim.Read(dir, 10, "process")
	if err != nil || len(history.Entries) != 1 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	entry := history.Entries[0]
	if entry.ID != "tree-1" || entry.MachineID != "host-1" || entry.Status != "cancelled" || entry.At.IsZero() {
		t.Fatal(entry)
	}
}

func TestReceiptKeepsPartialAccountingWithoutSensitiveText(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DRIGA_DATA_DIR", dir)
	outcome := KillOutcome{ID: "tree-1", MachineID: "host-1", Status: "partial", Reason: "SECRET_REASON", Error: &view.ErrorRecord{Code: "kill_failed", Message: "SECRET_COMMAND"}, Result: &kill.Result{Signaled: []int32{42, 43}, Stopped: []int32{42}, Survivors: []int32{43}, Unknown: []int32{44}, Reason: "SECRET_PATH", Before: derive.MemoryUsage{Processes: 3, Memory: 300, Measure: "rss", RSS: 300}, StoppedUsage: derive.MemoryUsage{Processes: 1, Memory: 100, Measure: "rss", RSS: 100}}}
	if err := recordKillOutcome(time.Now(), outcome); err != nil {
		t.Fatal(err)
	}
	h, err := reclaim.Read(dir, 10, "process")
	if err != nil || len(h.Entries) != 1 {
		t.Fatalf("%+v %v", h, err)
	}
	e := h.Entries[0]
	if len(e.Signaled) != 2 || len(e.Stopped) != 1 || len(e.Survivors) != 1 || len(e.Unknown) != 1 || e.Before.Memory != 300 || e.StoppedUsage.Memory != 100 || e.ErrorCode != "kill_failed" {
		t.Fatalf("%+v", e)
	}
	b, err := os.ReadFile(filepath.Join(dir, "reclaim.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("sensitive text entered durable receipt")
	}
}

func TestHistoryFailurePreservesOutcome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(dir, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_DATA_DIR", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, _ := (KillOptions{}).Execute(ctx, Environment{Stdout: io.Discard}, config.Config{}, nil, []model.Orphan{{ID: "tree-1"}})
	if len(report.Outcomes) != 1 || report.Outcomes[0].Status != "cancelled" || len(report.Warnings) != 1 {
		t.Fatalf("%+v", report)
	}
}
