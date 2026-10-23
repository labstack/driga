package chat

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestBrokerWaitWakesAllStreamsAndCancels(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	s, _ := b.Read(ctx, row.ID, 0)
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			s, err := b.Wait(ctx, row.ID, s.Cursor)
			if err == nil && s.Mode != Auto {
				err = fmt.Errorf("mode %s", s.Mode)
			}
			done <- err
		}()
	}
	if err := b.SetMode(ctx, row.ID, Auto); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("missed broker event")
		}
	}
	s, _ = b.Read(ctx, row.ID, 0)
	cancelCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := b.Wait(cancelCtx, row.ID, s.Cursor); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// A stream waiting on an evicted conversation must also wake and reload.
	wait := make(chan error, 1)
	go func() { _, err := b.Wait(ctx, row.ID, s.Cursor); wait <- err }()
	b.Close()
	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("Close left wait alive")
	}
}
