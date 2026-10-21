package cli

import (
	"os"
	"runtime/pprof"
	"sync"
	"time"
)

// startCPUProfile records the collector's CPU use into path for d and writes
// allocations to path + ".alloc", so the live service can be measured in place.
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
	var once sync.Once
	stop := func() {
		once.Do(func() {
			pprof.StopCPUProfile()
			f.Close()
			pprof.Lookup("allocs").WriteTo(alloc, 0)
			alloc.Close()
		})
	}
	t := time.AfterFunc(d, stop)
	return func() { t.Stop(); stop() }, nil
}
