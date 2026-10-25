package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestServiceRunOwnerFlag(t *testing.T) {
	for _, kind := range []string{"", "service", "manual"} {
		args := []string{"service", "run"}
		want := kind
		if kind == "" {
			want = "manual"
		} else {
			args = append(args, "--owner="+kind)
		}
		root, _, err := parseCommand(t, args...)
		if err != nil || root.Service.Run.Owner != want {
			t.Fatalf("%v: owner=%q, err=%v", args, root.Service.Run.Owner, err)
		}
	}
	if _, _, err := parseCommand(t, "service", "run", "--owner=app"); err == nil {
		t.Fatal("the app is no longer an owner")
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"service", "run", "--help"}, "test", strings.NewReader(""), &out, &diagnostic); code != 0 {
		t.Fatalf("help: code=%d, %s", code, &diagnostic)
	}
	if strings.Contains(out.String(), "--owner") {
		t.Fatal("owner flag appears in help")
	}
}
