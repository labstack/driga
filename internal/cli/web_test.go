package cli

import (
	"bytes"
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
