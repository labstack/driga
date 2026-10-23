package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestTopRequiresInteractiveTerminal(t *testing.T) {
	readEnv(t)
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	for _, input := range []io.Reader{null, strings.NewReader("")} {
		var out, diagnostic bytes.Buffer
		code := run([]string{"top"}, "test", input, &out, &diagnostic)
		if code != 2 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "driga top needs an interactive terminal; use driga agents or --json") {
			t.Fatalf("%d: %s", code, &diagnostic)
		}
	}
}
