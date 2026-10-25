package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWebIsRetired(t *testing.T) {
	readEnv(t)
	for _, args := range [][]string{{"web"}, {"web", "--listen", "127.0.0.1:0"}, {"web", "--open"}} {
		var out, errout bytes.Buffer
		code := run(args, "test", strings.NewReader(""), &out, &errout)
		if code == 0 || !strings.Contains(errout.String(), "driga web was replaced by the desktop app; run driga app") || out.Len() != 0 {
			t.Errorf("%v: %d %q %q", args, code, &out, &errout)
		}
	}
}
