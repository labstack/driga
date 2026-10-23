package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebRejectsNonLoopback(t *testing.T) {
	readEnv(t)
	for _, addr := range []string{"0.0.0.0:0", "192.0.2.1:8080", "[::]:0", "example.com:80", ":8080"} {
		var out, errout bytes.Buffer
		code := run([]string{"web", "--listen", addr}, "test", strings.NewReader(""), &out, &errout)
		if code == 0 || !strings.Contains(errout.String(), "loopback") {
			t.Errorf("%s: %d %s", addr, code, &errout)
		}
	}
}

func BenchmarkWebIdleHeartbeat(b *testing.B) {
	b.Setenv("DRIGA_DATA_DIR", b.TempDir())
	b.ReportAllocs()
	for range b.N {
		webHeartbeat(false)
	}
}

func TestWebHeartbeatRequiresBrowser(t *testing.T) {
	readEnv(t)
	path := filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "viewer.heartbeat")
	webHeartbeat(false)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("idle web process kept viewer cadence", err)
	}
	webHeartbeat(true)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	webHeartbeat(false)
	next, err := os.Stat(path)
	if err != nil || !next.ModTime().Equal(info.ModTime()) {
		t.Fatal("disconnected browser refreshed heartbeat", err)
	}
}
