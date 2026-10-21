package config

import (
	"path/filepath"
	"testing"
)

func TestInboxStrictEnvironment(t *testing.T) {
	t.Setenv("DRIGA_INBOX_ALERTS", "waiting")
	t.Setenv("DRIGA_INBOX_ALERT_AFTER", "0s")
	t.Setenv("DRIGA_INBOX_ALERT_REPEAT", "5m")
	t.Setenv("DRIGA_INBOX_JUMP_TERMINAL", "false")
	p := filepath.Join(t.TempDir(), "missing.toml")
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_INBOX_ALERTS", "sometimes")
	if _, err := Load(p); err == nil {
		t.Fatal("invalid alerts accepted")
	}
}
