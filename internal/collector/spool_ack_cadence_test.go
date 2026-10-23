package collector

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestDaemonDrainsSpoolSoonAfterAck(t *testing.T) {
	d, _, p, a := setup(t)
	dir := config.DataDir()
	// An existing day file catches append detection as well as creation.
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Event: "ack", Message: "initial"}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	a.collect = func(context.Context) { ready <- struct{}{} }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	var stopped sync.Once
	stop := func() {
		stopped.Do(func() {
			cancel()
			if err := <-done; err != nil {
				t.Error(err)
			}
		})
	}
	defer stop()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not start")
	}
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := st.Meta("ack:initial"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial pass not committed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Event: "ack", Message: "new"}); err != nil {
		t.Fatal(err)
	}
	for time.Since(started) < 1500*time.Millisecond {
		if _, err := st.Meta("ack:new"); err == nil {
			stop()
			if p.procCalls != 1 || len(a.envs) != 1 {
				t.Fatal("spool wake collected other sources")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ack receipt not written within idle wake interval")
}
