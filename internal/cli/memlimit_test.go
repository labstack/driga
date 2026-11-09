package cli

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func runtimeLimitFixture(t *testing.T) {
	t.Helper()
	oldLimit := debug.SetMemoryLimit(128 << 20)
	oldGC := debug.SetGCPercent(93)
	oldProcs := runtime.GOMAXPROCS(4)
	t.Cleanup(func() {
		debug.SetMemoryLimit(oldLimit)
		debug.SetGCPercent(oldGC)
		runtime.GOMAXPROCS(oldProcs)
	})
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv("GOGC", "")
	t.Setenv("GOMAXPROCS", "")
	if err := os.Unsetenv("GOMAXPROCS"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_CONFIG", configPath)
	t.Setenv("DRIGA_DATA_DIR", t.TempDir())
	t.Setenv("DRIGA_SPOOL_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
}

func assertRuntimeLimits(t *testing.T, wantMemory int64, wantGC uint64, wantProcs int) {
	t.Helper()
	readings := []metrics.Sample{{Name: "/gc/gogc:percent"}}
	metrics.Read(readings)
	if limit, gc, procs := debug.SetMemoryLimit(-1), readings[0].Value.Uint64(), runtime.GOMAXPROCS(0); limit != wantMemory || gc != wantGC || procs != wantProcs {
		t.Fatalf("runtime limits: memory=%d, GOGC=%d, GOMAXPROCS=%d; want %d, %d, %d", limit, gc, procs, wantMemory, wantGC, wantProcs)
	}
}

func TestServiceRunRestoresRuntimeLimitsAfterValidationError(t *testing.T) {
	runtimeLimitFixture(t)
	serve := true
	err := (ServiceRunCmd{Serve: &serve, Listen: "0.0.0.0:7411"}).Run(&Globals{})
	if err == nil || !strings.Contains(err.Error(), "allow_any_address") {
		t.Fatalf("expected private-listen validation error, got %v", err)
	}
	assertRuntimeLimits(t, 128<<20, 93, 4)
}

func TestServiceRunRestoresRuntimeLimitsAfterOnce(t *testing.T) {
	runtimeLimitFixture(t)
	previous := newCollectDeps
	t.Cleanup(func() { newCollectDeps = previous })
	called := false
	newCollectDeps = func(cfg config.Config, _ attention.Notifier) (collector.Deps, error) {
		called = true
		// --once keeps caller parallelism; both collection modes use the same
		// memory defaults until collection and all cleanup have completed.
		assertRuntimeLimits(t, collectorMemoryLimit, collectorGCPercent, 4)
		sigs, err := signatures.Load(nil)
		return collector.Deps{Cfg: cfg, Plat: &cliSeqPlatform{snaps: [][]model.Proc{{}}}, Sigs: sigs,
			Git: gitscan.NewScanner(1), SpoolDir: config.SpoolDir(), Now: time.Now, State: collector.NewState()}, err
	}
	serve := false
	var out bytes.Buffer
	if err := (ServiceRunCmd{Once: true, Serve: &serve}).Run(&Globals{Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if !called || !strings.Contains(out.String(), "Collected 0 sessions, 0 processes, 1 samples") {
		t.Fatalf("once did not complete fake collection: %s", &out)
	}
	assertRuntimeLimits(t, 128<<20, 93, 4)
}

func TestCollectorRuntimeRestoreLeavesUserSettingsAlone(t *testing.T) {
	runtimeLimitFixture(t)
	t.Setenv("GOMEMLIMIT", "128MiB")
	t.Setenv("GOGC", "93")
	t.Setenv("GOMAXPROCS", "4")
	restoreMemory, restoreProcs := applyCollectorMemoryLimit(), applyCollectorParallelism()
	t.Cleanup(restoreMemory)
	t.Cleanup(restoreProcs)
	assertRuntimeLimits(t, 128<<20, 93, 4)
	// A setting this scope did not own must not be reset when it returns.
	debug.SetMemoryLimit(192 << 20)
	debug.SetGCPercent(94)
	runtime.GOMAXPROCS(5)
	restoreProcs()
	restoreMemory()
	assertRuntimeLimits(t, 192<<20, 94, 5)
}

func TestCollectorParallelismDefault(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(old) })
	t.Setenv("GOMAXPROCS", "")
	if err := os.Unsetenv("GOMAXPROCS"); err != nil {
		t.Fatal(err)
	}

	restore := applyCollectorParallelism()
	t.Cleanup(restore)
	if got, want := runtime.GOMAXPROCS(0), min(2, runtime.NumCPU()); got != want {
		t.Fatalf("default GOMAXPROCS = %d, want %d", got, want)
	}
	restore()
	if got := runtime.GOMAXPROCS(0); got != 4 {
		t.Fatalf("GOMAXPROCS after restore = %d, want 4", got)
	}
}

func TestCollectorParallelismUserEnv(t *testing.T) {
	old := runtime.GOMAXPROCS(3)
	t.Cleanup(func() { runtime.GOMAXPROCS(old) })
	for _, value := range []string{"8", ""} {
		t.Run("GOMAXPROCS="+value, func(t *testing.T) {
			t.Setenv("GOMAXPROCS", value)
			t.Cleanup(applyCollectorParallelism())
			if got := runtime.GOMAXPROCS(0); got != 3 {
				t.Fatalf("user GOMAXPROCS must win, value changed to %d", got)
			}
		})
	}
}

// The collector is a long-lived daemon with a ~5 MB live heap; without a soft
// limit the runtime keeps far more resident than the 50 MB budget allows.
func TestCollectorMemoryLimitDefault(t *testing.T) {
	old := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(old) })

	t.Setenv("GOMEMLIMIT", "")
	debug.SetMemoryLimit(math.MaxInt64)
	t.Cleanup(applyCollectorMemoryLimit())
	if got := debug.SetMemoryLimit(-1); got != collectorMemoryLimit {
		t.Fatalf("default limit = %d, want %d", got, collectorMemoryLimit)
	}

	t.Setenv("GOMEMLIMIT", "200MiB")
	debug.SetMemoryLimit(math.MaxInt64)
	t.Cleanup(applyCollectorMemoryLimit())
	if got := debug.SetMemoryLimit(-1); got != math.MaxInt64 {
		t.Fatalf("user GOMEMLIMIT must win, limit changed to %d", got)
	}
}

// Measured on a serving collector with two viewers: GOGC=50 keeps peak RSS
// about 4 MB lower than the default at a negligible GC cost (live heap ~8 MB).
func TestCollectorGCPercentDefault(t *testing.T) {
	old := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(old) })

	t.Setenv("GOGC", "")
	t.Cleanup(applyCollectorMemoryLimit())
	if got := debug.SetGCPercent(100); got != collectorGCPercent {
		t.Fatalf("default GC percent = %d, want %d", got, collectorGCPercent)
	}

	t.Setenv("GOGC", "200")
	t.Cleanup(applyCollectorMemoryLimit())
	if got := debug.SetGCPercent(100); got != 100 {
		t.Fatalf("user GOGC must win, GC percent changed to %d", got)
	}
}
