package fleet

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// Hold both responses at their first write, then consume without retaining the
// body. This includes memory retained for a slow reader, not reader buffers.
type heapSnapshotWriter struct {
	header http.Header
	ready  chan<- struct{}
	resume <-chan struct{}
	once   sync.Once
	bytes  int
}

func (w *heapSnapshotWriter) Header() http.Header { return w.header }
func (w *heapSnapshotWriter) WriteHeader(int)     {}
func (w *heapSnapshotWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { w.ready <- struct{}{}; <-w.resume })
	w.bytes += len(p)
	return len(p), nil
}

func TestSnapshotStreamsWithoutMaterialising(t *testing.T) {
	// Isolate SQLite caches and previous tests' heap high-water marks.
	if os.Getenv("DRIGA_J4_HEAP_CHILD") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestSnapshotStreamsWithoutMaterialising$", "-test.v")
		cmd.Env = append(os.Environ(), "DRIGA_J4_HEAP_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("snapshot heap: %v\n%s", err, out)
		}
		t.Logf("%s", out)
		return
	}
	s, st, id := serverFixture(t)
	const rowBytes = 64 << 10
	const rows = 320 // 20 MiB of exported, non-text-gated event metadata.
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		kind := strings.Repeat("x", rowBytes)
		for i := range rows {
			if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: fmt.Sprint(i), TS: time.Now(), Kind: kind}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	debug.SetGCPercent(10)
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	peak.Store(base.HeapInuse)
	sample := func() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		for old := peak.Load(); m.HeapInuse > old; old = peak.Load() {
			if peak.CompareAndSwap(old, m.HeapInuse) {
				break
			}
		}
	}
	stop, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				sample()
			case <-stop:
				return
			}
		}
	}()
	ready, resume := make(chan struct{}, 2), make(chan struct{})
	done := make(chan *heapSnapshotWriter, 2)
	for range 2 {
		go func() {
			w := &heapSnapshotWriter{header: make(http.Header), ready: ready, resume: resume}
			s.ServeHTTP(w, request(s, id, "GET", "/v1/snapshot"))
			done <- w
		}()
	}
	for range 2 {
		<-ready
	}
	sample()
	close(resume)
	for range 2 {
		if w := <-done; w.bytes < rows*rowBytes {
			t.Errorf("incomplete snapshot: %d bytes", w.bytes)
		}
	}
	sample()
	close(stop)
	<-sampled
	// Two bounded 256 KiB row batches plus 12 MiB for SQL, JSON and GC slack.
	const limit = 2*(256<<10) + (12 << 20)
	delta := peak.Load() - base.HeapInuse
	t.Logf("two 20 MiB snapshots: additional peak HeapInuse = %.2f MiB (limit %.2f MiB)", float64(delta)/(1<<20), float64(limit)/(1<<20))
	if delta >= limit {
		t.Fatalf("snapshot materialised: peak growth %d bytes, want < %d", delta, limit)
	}
}

var _ io.Writer = (*heapSnapshotWriter)(nil)

type servingTestTicker struct {
	delay time.Duration
	c     chan time.Time
	ready chan<- struct{}
	once  sync.Once
}

func (t *servingTestTicker) Chan() <-chan time.Time {
	if t.delay == 15*time.Second {
		t.once.Do(func() { t.ready <- struct{}{} })
	}
	return t.c
}
func (*servingTestTicker) Stop() {}

func TestIdleServingTimerCount(t *testing.T) {
	for _, presence := range []bool{false, true} {
		t.Run(fmt.Sprintf("presence=%t", presence), func(t *testing.T) {
			s, st, id := serverFixture(t)
			var mu sync.Mutex
			var ticks []*servingTestTicker
			ready := make(chan struct{}, 2)
			s.newTicker = func(d time.Duration) ticker {
				f := &servingTestTicker{delay: d, c: make(chan time.Time, 1), ready: ready}
				mu.Lock()
				ticks = append(ticks, f)
				mu.Unlock()
				return f
			}
			epoch, _, _, err := st.FeedBounds()
			if err != nil {
				t.Fatal(err)
			}
			path := "/v1/events"
			if presence {
				path += "?presence=1"
			}
			var writers []*streamWriter
			for range 2 {
				w, _, _ := stream(t, s, id, epoch+":0", path)
				writers = append(writers, w)
				nextFrame(t, w)
			}
			for range 2 {
				select {
				case <-ready:
				case <-time.After(time.Second):
					t.Fatal("stream did not start timers")
				}
			}
			mu.Lock()
			active := append([]*servingTestTicker(nil), ticks...)
			mu.Unlock()
			// Advance a fake 60s, counting every scheduled fire (including any
			// extra presence/polling timers). Each ping is acknowledged by flush.
			fires := 0
			for second := 1; second <= 60; second++ {
				at := time.Unix(int64(second), 0)
				for _, tick := range active {
					if time.Duration(second)*time.Second%tick.delay != 0 {
						continue
					}
					fires++
					tick.c <- at
				}
				if second%15 == 0 {
					for _, w := range writers {
						if got := nextFrame(t, w); got != ": ping\n\n" {
							t.Fatalf("unexpected frame: %q", got)
						}
					}
				}
			}
			t.Logf("2 idle streams / 60s: %d timer fires", fires)
			if fires > 8 {
				t.Fatalf("idle serving: %d timer fires, want <= 2*4 pings", fires)
			}
		})
	}
}
