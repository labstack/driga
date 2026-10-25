package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWebIsGone(t *testing.T) {
	readEnv(t)
	var out, errout bytes.Buffer
	code := run([]string{"web"}, "test", strings.NewReader(""), &out, &errout)
	if code != 2 || !strings.Contains(errout.String(), "unexpected argument web") {
		t.Fatalf("driga web: %d %q %q", code, &out, &errout)
	}
}

func TestHelpListsApp(t *testing.T) {
	var out, errout bytes.Buffer
	if code := run([]string{"--help"}, "test", nil, &out, &errout); code != 0 {
		t.Fatalf("--help: %d %s", code, &errout)
	}
	help := out.String()
	if !strings.Contains(help, "\n  app ") || strings.Contains(help, "\n  web ") {
		t.Fatal(help)
	}
}
