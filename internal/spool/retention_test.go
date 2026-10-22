package spool

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestSpoolPruneOldDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for _, days := range []int{0, 30, 31} {
		if err := Append(dir, model.HookEvent{TS: now.AddDate(0, 0, -days)}); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(dir, "notes.jsonl")
	if err := os.WriteFile(other, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Prune(dir, now); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("retention: %v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(dir, now.AddDate(0, 0, -31).Format("20060102")+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old day retained: %v", err)
	}
}
