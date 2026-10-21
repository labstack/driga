package logfile

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestTextHandlerUTC(t *testing.T) {
	var out bytes.Buffer
	record := slog.NewRecord(time.Date(2026, 10, 19, 12, 0, 0, 0, time.FixedZone("local", -7*3600)), slog.LevelError, "failed", 0)
	record.Add("event", "hook_failed", "err", "untrusted\nmessage")
	if err := TextHandler(&out).Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "time=2026-10-19T19:00:00.000Z") || strings.Count(out.String(), "\n") != 1 {
		t.Fatal(out.String())
	}
}
