package spool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestLegacyMigrationRetainsInFlightWriter(t *testing.T) {
	old, dest := t.TempDir(), t.TempDir()
	name := "20261019.jsonl"
	f, err := os.OpenFile(filepath.Join(old, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	load := func(string) (model.Offset, error) { return model.Offset{}, nil }
	save := func(string, model.Offset) error { return nil }
	if err := Migrate(old, dest, load, save); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(f, `{"session_id":"in-flight","event":"Stop"}`); err != nil {
		t.Fatal(err)
	}
	events, _, err := Drain(dest, nil)
	if err != nil || len(events) != 1 || events[0].SessionID != "in-flight" {
		t.Fatal(events, err)
	}
}

func TestLegacyMigrationOffsetsAndRetry(t *testing.T) {
	for _, copyMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("copy=%t", copyMode), func(t *testing.T) {
			previous := linkLegacySpool
			t.Cleanup(func() { linkLegacySpool = previous })
			if copyMode {
				linkLegacySpool = func(string, string) error { return syscall.EXDEV }
			}
			testLegacyMigrationOffsetsAndRetry(t)
		})
	}
}

func testLegacyMigrationOffsetsAndRetry(t *testing.T) {
	old, dest := filepath.Join(t.TempDir(), "old"), filepath.Join(t.TempDir(), "new")
	at := time.Now().UTC()
	if err := Append(old, model.HookEvent{TS: at, Event: "Stop", SessionID: "consumed"}); err != nil {
		t.Fatal(err)
	}
	_, offsets, err := Drain(old, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Append(old, model.HookEvent{TS: at, Event: "Stop", SessionID: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(dest, model.HookEvent{TS: at, Event: "Stop", SessionID: "new"}); err != nil {
		t.Fatal(err)
	}
	load := func(name string) (model.Offset, error) { return offsets[name], nil }
	saved := map[string]model.Offset{}
	fail := true
	save := func(name string, o model.Offset) error {
		if fail {
			return errors.New("checkpoint interrupted")
		}
		saved[name] = o
		return nil
	}
	if err := Migrate(old, dest, load, save); err == nil {
		t.Fatal("wanted checkpoint failure")
	}
	if files, _ := filepath.Glob(filepath.Join(old, "*.jsonl")); len(files) != 1 {
		t.Fatal(files)
	}
	fail = false
	if err := Migrate(old, dest, load, save); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(old, dest, load, save); err != nil {
		t.Fatal(err)
	}
	events, next, err := Drain(dest, saved)
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	for _, ev := range events {
		if ev.SessionID == "consumed" {
			t.Fatal("replayed acknowledged bytes")
		}
	}
	events, _, err = Drain(dest, next)
	if err != nil || len(events) != 0 {
		t.Fatal(events, err)
	}
	if files, _ := filepath.Glob(filepath.Join(old, "*.jsonl")); len(files) != 0 {
		t.Fatal(files)
	}
	if _, err := os.Stat(filepath.Join(old, "README")); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMigrationPreservesPartialAndRefusesCollision(t *testing.T) {
	old, dest := t.TempDir(), t.TempDir()
	name := "20261019.jsonl"
	source := filepath.Join(old, name)
	if err := os.WriteFile(source, []byte(`{"session_id":"partial"`), 0600); err != nil {
		t.Fatal(err)
	}
	load := func(string) (model.Offset, error) { return model.Offset{}, nil }
	save := func(string, model.Offset) error { return nil }
	target := filepath.Join(dest, "legacy-"+name)
	if err := os.WriteFile(target, []byte("conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(old, dest, load, save); err == nil {
		t.Fatal("overwrote destination")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(old, dest, load, save); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != `{"session_id":"partial"` {
		t.Fatal(string(b), err)
	}
}
