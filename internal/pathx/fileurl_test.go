package pathx

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestFileURIs(t *testing.T) {
	for _, tc := range []struct{ os, p, want string }{
		{"linux", "/work/a b.db", "file:///work/a%20b.db"},
		{"linux", "relative/a#?%.db", "file:relative/a%23%3F%25.db"},
		{"windows", `C:\Work\子 folder\data.db`, "file:///C:/Work/%E5%AD%90%20folder/data.db"},
		{"windows", `\\server\share\data.db`, "file:////server/share/data.db"},
	} {
		if got := fileURLForOS(tc.p, tc.os); got != tc.want {
			t.Fatalf("%q: %q", tc.p, got)
		}
	}
}

func TestFileURLRelativeEscapes(t *testing.T) {
	t.Chdir(t.TempDir())
	p := "a/../b/space #?%.db"
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(abs)
	if len(want) > 1 && want[1] == ':' {
		want = "/" + want
	}
	u, err := url.Parse(FileURL(p))
	if err != nil || u.Host != "" || u.Path != want || u.RawQuery != "" || u.Fragment != "" {
		t.Fatalf("unsafe file URI: %+v %v", u, err)
	}
}
