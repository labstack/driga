package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestMissingSessionIDs(t *testing.T) {
	readEnv(t)
	readDB(t, func(*store.Tx) error { return nil })
	for _, cmd := range []string{"agent", "flow"} {
		for _, jsonMode := range []bool{false, true} {
			args := []string{cmd, "999999999"}
			if cmd == "agent" {
				args = []string{"agents", "show", "999999999"}
			}
			if jsonMode {
				args = append(args, "--json")
			}
			var out, diagnostic bytes.Buffer
			code := run(args, "test", nil, &out, &diagnostic)
			if code != 1 || !strings.Contains(diagnostic.String(), "agent 999999999 not found; list ids with driga agents") {
				t.Fatalf("%v: %d %s %s", args, code, &out, &diagnostic)
			}
			if jsonMode && !strings.Contains(out.String(), `"code":"not_found"`) {
				t.Fatal(out.String())
			}
		}
	}
}
