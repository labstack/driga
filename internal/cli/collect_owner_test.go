package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCollectOwnerFlag(t *testing.T) {
	for _, kind := range []string{"", "app", "service", "manual"} {
		args := []string{"collect"}
		want := kind
		if kind == "" {
			want = "manual"
		} else {
			args = append(args, "--owner="+kind)
		}
		root, _, err := parseCommand(t, args...)
		if err != nil || root.Collect.Owner != want {
			t.Fatalf("%v: owner=%q, err=%v", args, root.Collect.Owner, err)
		}
	}
	if _, _, err := parseCommand(t, "collect", "--owner=unknown"); err == nil {
		t.Fatal("unknown owner accepted")
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"collect", "--help"}, "test", strings.NewReader(""), &out, &diagnostic); code != 0 {
		t.Fatalf("help: code=%d, %s", code, &diagnostic)
	}
	if strings.Contains(out.String(), "--owner") {
		t.Fatal("owner flag appears in help")
	}
}
