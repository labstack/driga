package collector

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPassCollectsSourcesBeforeTransactionConcurrently(t *testing.T) {
	d, st, _, a := setup(t)
	b := &fakeAdapter{name: "codex"}
	d.Adapters = append(d.Adapters, b)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	var readErr error
	collect := func(ctx context.Context) {
		readCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err := st.DataVersion(readCtx)
		mu.Lock()
		if err != nil {
			readErr = err
		}
		mu.Unlock()
		ready <- struct{}{}
		<-release
	}
	a.collect, b.collect = collect, collect
	done := make(chan error, 1)
	go func() { _, err := Pass(context.Background(), d, st, Plan{Procs: true}); done <- err }()
	concurrent := true
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-time.After(300 * time.Millisecond):
			concurrent = false
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !concurrent || readErr != nil {
		t.Fatalf("sources concurrent=%v, read outside transaction=%v", concurrent, readErr)
	}
}
