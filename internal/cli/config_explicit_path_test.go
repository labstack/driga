package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestConfigShowMissingExplicitPath(t *testing.T) {
	readEnv(t)
	if err := os.Remove(os.Getenv("DRIGA_CONFIG")); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	code := run([]string{"config", "show"}, "test", nil, &out, &diagnostic)
	if code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), os.Getenv("DRIGA_CONFIG")) {
		t.Fatalf("%d %s %s", code, &out, &diagnostic)
	}
}
