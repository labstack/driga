package cli

import (
	"encoding/json"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

// startCPUProfile records CPU use into path for d, cumulative allocations to
// path + ".alloc", a final collected heap to ".heap", and memory/goroutine
// samples every five seconds to ".mem.jsonl".
// The returned stop is idempotent and flushes early on shutdown.
func startCPUProfile(path string, d time.Duration) (func(), error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	alloc, err := os.Create(path + ".alloc")
	if err != nil {
		f.Close()
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		alloc.Close()
		return nil, err
	}
	memory, err := os.Create(path + ".mem.jsonl")
	if err != nil {
		pprof.StopCPUProfile()
		f.Close()
		alloc.Close()
		return nil, err
	}
	done, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		defer memory.Close()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		sample := func() {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			_ = json.NewEncoder(memory).Encode(struct {
				At                                      time.Time
				HeapAlloc, HeapObjects, TotalAlloc, Sys uint64
				NumGC                                   uint32
				Goroutines                              int
			}{time.Now(), m.HeapAlloc, m.HeapObjects, m.TotalAlloc, m.Sys, m.NumGC, runtime.NumGoroutine()})
		}
		sample()
		for {
			select {
			case <-ticker.C:
				sample()
			case <-done:
				sample()
				return
			}
		}
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			pprof.StopCPUProfile()
			close(done)
			<-sampled
			f.Close()
			pprof.Lookup("allocs").WriteTo(alloc, 0)
			alloc.Close()
			if heap, err := os.Create(path + ".heap"); err == nil {
				runtime.GC()
				_ = pprof.WriteHeapProfile(heap)
				_ = heap.Close()
			}
		})
	}
	t := time.AfterFunc(d, stop)
	return func() { t.Stop(); stop() }, nil
}
