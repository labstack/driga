package cleanup

import (
	"strings"
	"testing"
)

func TestProjectSlugClaude21303(t *testing.T) {
	// Pinned by running the N and Cde functions extracted from Claude Code
	// 2.1.303 with osascript -l JavaScript. All three paths have 250 UTF-16 units.
	for _, tc := range []struct{ path, want string }{
		{"/" + strings.Repeat("a", 249), "-" + strings.Repeat("a", 199) + "-wkpu26"},
		{"/tmp/" + strings.Repeat("Ab_9/", 49), "-tmp-" + strings.Repeat("Ab-9-", 39) + "-hulnjn"},
		{"/work/" + strings.Repeat("😀é/", 61), "-work-" + strings.Repeat("-", 194) + "-u6959o"},
		// Untruncated ASCII names retain their existing encoding, including at 200.
		{"/a_b.c/d 9", "-a-b-c-d-9"},
		{"/" + strings.Repeat("x", 199), "-" + strings.Repeat("x", 199)},
		// The JS regex has no Unicode flag: each surrogate becomes a hyphen.
		{"/😀é", "----"},
	} {
		if got := projectSlug(tc.path); got != tc.want {
			t.Errorf("slug for %q:\n got %s\nwant %s", tc.path, got, tc.want)
		}
	}
}
