package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCollectParentFDFlag(t *testing.T) {
	root, _, err := parseCommand(t, "collect", "--parent-fd=4")
	if err != nil {
		t.Fatal(err)
	}
	if fd := root.Collect.ParentFD; fd == nil || *fd != 4 {
		t.Fatalf("parent fd = %v, want 4", fd)
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"collect", "--help"}, "test", strings.NewReader(""), &out, &diagnostic); code != 0 {
		t.Fatalf("help: code=%d, %s", code, &diagnostic)
	}
	if strings.Contains(out.String(), "--parent-fd") {
		t.Fatal("parent fd flag appears in help")
	}
	fd := -1
	if err := (CollectCmd{ParentFD: &fd}).Run(nil); err == nil {
		t.Fatal("negative parent fd accepted")
	}
}
