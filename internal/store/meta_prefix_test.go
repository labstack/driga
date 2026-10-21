package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Alert planning reads one timestamp per inbox item; loading a key range in
// one query replaces thousands of single-key reads per collector pass.
func TestMetaPrefixReturnsOnlyThatRange(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "driga.db"), ReadWrite)
	if err := s.Tx(context.Background(), func(tx *Tx) error {
		for k, v := range map[string]string{"inbox_alert:a": "1", "inbox_alert:b": "2", "inbox_alert_session:a": "3", "inbox_alerts": "x", "baseline_at": "y"} {
			if err := tx.SetMeta(k, v); err != nil {
				return err
			}
		}
		got, err := tx.MetaPrefix("inbox_alert:")
		if err != nil {
			return err
		}
		if len(got) != 2 || got["a"] != "1" || got["b"] != "2" {
			t.Fatalf("got %v", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
