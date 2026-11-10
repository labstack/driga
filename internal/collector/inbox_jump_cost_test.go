package collector

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestWriteInboxJumpsDoesNotReadPayloads(t *testing.T) {
	measure := func(payloadSize, sessionCount int) testing.BenchmarkResult {
		st, err := store.Open("", store.Memory)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		payload := strings.Repeat("x", payloadSize)
		ss := make([]model.Session, sessionCount)
		for i := range ss {
			ss[i] = model.Session{ID: int64(2000 + i), SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i)}}
		}
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "cost"}})
			if err != nil {
				return err
			}
			for i := range 64 {
				if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprintf("item-%03d", i), SessionID: id, Kind: "finished", TS: time.Unix(1, 0), Summary: payload, Detail: payload, Jump: &model.InboxJump{Kind: "command", Command: payload}}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// Keep item count and identities constant. Only unused payloads change;
		// none of these sessions needs a destination update in this pass.
		return testing.Benchmark(func(b *testing.B) {
			for range b.N {
				if err := st.Tx(context.Background(), func(tx *store.Tx) error { return writeInboxJumps(tx, ss, nil) }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	small, large := measure(8, 0), measure(4096, 0)
	t.Logf("64 inbox identities: small payload %d B/op, %d allocs/op; 4KiB summary/detail/jump %d B/op, %d allocs/op", small.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocedBytesPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > 2*small.AllocedBytesPerOp() {
		t.Fatal("jump destination refresh allocated unused inbox payloads")
	}
	withSessions := measure(8, 1600)
	t.Logf("1600 session associations: %d B/op", withSessions.AllocedBytesPerOp())
	// Only a session identity and a destination pointer need to travel through
	// this join. Copying complete observations grows with unrelated session data.
	if withSessions.AllocedBytesPerOp()-small.AllocedBytesPerOp() > 128*1600 {
		t.Fatal("jump destination refresh copied unrelated session observations")
	}
}
