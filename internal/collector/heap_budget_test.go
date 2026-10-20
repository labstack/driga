package collector

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/store"
)

func TestInitialImportPeakHeap(t *testing.T) {
	// Measure a fresh process so earlier tests' SQLite caches and large-line
	// buffers cannot change the initial-import peak depending on test order.
	if os.Getenv("DRIGA_COLLECTOR_HEAP_CHILD") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestInitialImportPeakHeap$", "-test.v")
		cmd.Env = append(os.Environ(), "DRIGA_COLLECTOR_HEAP_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("initial import: %v\n%s", err, out)
		}
		t.Logf("%s", out)
		return
	}
	d, st, _, _ := setup(t)
	d.Adapters = []adapter.Adapter{claude.New(), codex.New()}
	home := os.Getenv("HOME")
	// Fifty recent logs, split across both real adapters. Unknown fields model
	// transcript bulk without making retained observations grow with input size.
	prefix, suffix := `{"type":"progress","padding":"`, "\"}\n"
	line := prefix + strings.Repeat("x", (64<<10)-len(prefix)-len(suffix)) + suffix
	for i := range 50 {
		var path, header string
		if i%2 == 0 {
			path = filepath.Join(home, ".claude/projects/synthetic", fmt.Sprintf("session-%d.jsonl", i))
			header = fmt.Sprintf("{\"type\":\"ai-title\",\"aiTitle\":\"session-%d\"}\n", i)
		} else {
			path = filepath.Join(home, ".codex/sessions/2026/10/18", fmt.Sprintf("rollout-%d.jsonl", i))
			header = fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":\"session-%d\"}}\n", i)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(header)
		// Exactly 8 MiB of recent lines after identity, exercising skip-ahead
		// and its bounded header recovery as well as the streaming tail.
		for n := 0; err == nil && n < 8<<20; n += len(line) {
			_, err = f.WriteString(line)
		}
		if closeErr := f.Close(); err != nil || closeErr != nil {
			t.Fatalf("fixture write: %v, close: %v", err, closeErr)
		}
		if err := os.Chtimes(path, d.now(), d.now()); err != nil {
			t.Fatal(err)
		}
	}
	// Measure the live heap marked by each GC, not HeapInuse: under CPU
	// contention GC lags allocation and HeapInuse also counts garbage waiting
	// to be swept (9.9 to 40 MiB for the same input on a loaded machine). The
	// regression held ~120 MiB live, the fix a few MiB.
	debug.SetGCPercent(10)
	runtime.GC()
	var peak atomic.Uint64
	live := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
	sample := func() {
		metrics.Read(live)
		v := live[0].Value.Uint64()
		for old := peak.Load(); v > old; old = peak.Load() {
			if peak.CompareAndSwap(old, v) {
				break
			}
		}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				sample()
			case <-stop:
				return
			}
		}
	}()
	_, err := Pass(context.Background(), d, st, Plan{})
	sample()
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sessions(t, st)); got != 50 {
		t.Fatalf("imported %d sessions, want 50", got)
	}
	t.Logf("initial import peak live heap = %.2f MiB", float64(peak.Load())/(1<<20))
	if peak.Load() >= 30_000_000 {
		t.Fatalf("peak live heap = %d bytes, want < 30 MB", peak.Load())
	}
}

type daemonTestClock struct {
	mu       sync.Mutex
	now      time.Time
	requests chan *daemonTestTimer
}
type daemonTestTimer struct {
	delay time.Duration
	ticks chan time.Time
}

func (c *daemonTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *daemonTestClock) advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}
func (c *daemonTestClock) NewTimer(d time.Duration) daemonTimer {
	timer := &daemonTestTimer{delay: d, ticks: make(chan time.Time, 1)}
	c.requests <- timer
	return timer
}
func (t *daemonTestTimer) Chan() <-chan time.Time { return t.ticks }
func (t *daemonTestTimer) Stop()                  {}

func TestIdleDaemonTimerCount(t *testing.T) {
	for _, watched := range []bool{false, true} {
		t.Run(fmt.Sprintf("watched=%t", watched), func(t *testing.T) {
			d, _, platform, a := setup(t)
			clock := &daemonTestClock{now: d.now(), requests: make(chan *daemonTestTimer)}
			d.clock, d.Now = clock, clock.Now
			dir := t.TempDir()
			heartbeat := filepath.Join(dir, "viewer.heartbeat")
			touch := func() {
				if !watched {
					return
				}
				if err := os.WriteFile(heartbeat, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(heartbeat, clock.Now(), clock.Now()); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			a.collect = func(context.Context) {
				calls++
				if calls == 1 {
					// Initial import can exceed both the idle and watched cadence.
					clock.advance(75 * time.Second)
				}
			}
			touch()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- Run(ctx, d, dir) }()
			// Always unblock NewTimer before canceling if a failed assertion exits.
			defer func() {
				cancel()
				for {
					select {
					case err := <-done:
						if err != nil {
							t.Error(err)
						}
						return
					case timer := <-clock.requests:
						timer.ticks <- clock.Now()
					case <-time.After(5 * time.Second):
						t.Error("daemon did not stop")
						return
					}
				}
			}()
			fires := 0
			var until time.Time
			for {
				var timer *daemonTestTimer
				select {
				case timer = <-clock.requests:
				case <-time.After(5 * time.Second):
					t.Fatal("daemon did not request a timer")
				}
				if until.IsZero() {
					until = clock.Now().Add(30 * time.Second)
				}
				if timer.delay < time.Second {
					t.Fatalf("daemon requested a %v timer after collection; want >=1s", timer.delay)
				}
				if !clock.Now().Before(until) {
					break
				}
				clock.advance(timer.delay)
				touch()
				timer.ticks <- clock.Now()
				fires++
			}
			// The next timer request synchronizes completion of the previous pass.
			procs := platform.procCalls - 1 // exclude the initial import
			if fires > 30 || (watched && procs > 6) || (!watched && procs > 0) {
				t.Fatalf("30s: %d timer fires, %d process collections", fires, procs)
			}
			t.Logf("30s fake clock through Run: %d timer fires, %d process collections", fires, procs)
		})
	}
}

func TestShareDowngradeCleanedBeforeServe(t *testing.T) {
	d, _, _, a := setup(t)
	dir := t.TempDir()
	d.Cfg.Fleet.Serve = true
	d.Cfg.Fleet.Listen = "127.0.0.1:0"
	d.Cfg.Fleet.ShareText = "none"
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetFeedShare(feed.Snippets); err != nil {
			return err
		}
		if err := tx.ConfigureFeed(true); err != nil {
			return err
		}
		_, err := tx.AppendFeed("event", "old", []byte(`{"summary":"pre-downgrade secret"}`), d.now())
		return err
	})
	epoch, _, _, boundsErr := st.FeedBounds()
	st.Close()
	if err != nil || boundsErr != nil {
		t.Fatalf("seed: %v, bounds: %v", err, boundsErr)
	}
	serverID, err := fleet.LoadOrCreateIdentity(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := fleet.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fleet.SaveViewers(filepath.Join(dir, "fleet"), []fleet.Viewer{{DeviceID: viewer.DeviceID, Permissions: []string{"read"}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collecting := make(chan struct{})
	a.collect = func(ctx context.Context) { close(collecting); <-ctx.Done() }
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-collecting:
	case <-time.After(5 * time.Second):
		t.Fatal("first collection never started")
	}
	var address string
	deadline := time.Now().Add(5 * time.Second)
	for address == "" {
		if b, err := os.ReadFile(filepath.Join(dir, "fleet/listen")); err == nil {
			address = strings.TrimSpace(string(b))
		} else if time.Now().After(deadline) {
			t.Fatal("fleet never listened")
		} else {
			time.Sleep(time.Millisecond)
		}
	}
	pin, _ := fleet.ParseDeviceID(serverID.DeviceID)
	transport := &http.Transport{TLSClientConfig: fleet.PinnedTLS(viewer, func(fp [32]byte) bool { return fleet.EqualFingerprint(pin, fp) })}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+address+"/v1/events", nil)
	req.Header.Set("Last-Event-ID", epoch+":0")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status: %s", resp.Status)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			if line != "event: resnapshot" {
				t.Fatalf("server exposed retained feed before cleanup: %s", line)
			}
			return
		}
	}
	t.Fatalf("missing resnapshot: %v", scanner.Err())
}
