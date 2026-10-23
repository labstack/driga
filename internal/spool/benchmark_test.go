package spool

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func BenchmarkAppend(b *testing.B) {
	dir := b.TempDir()
	ev := model.HookEvent{TS: time.Unix(1792499400, 0), Adapter: "copilot", SessionID: "s", Event: "postToolUse"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Append(dir, ev); err != nil {
			b.Fatal(err)
		}
	}
}
