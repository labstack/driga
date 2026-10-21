package pathx

import "testing"

func TestFileURIs(t *testing.T) {
	for _, tc := range []struct{ os, p, want string }{
		{"linux", "/work/a b.db", "file:///work/a%20b.db"},
		{"windows", `C:\Work\子 folder\data.db`, "file:///C:/Work/%E5%AD%90%20folder/data.db"},
		{"windows", `\\server\share\data.db`, "file:////server/share/data.db"},
	} {
		if got := fileURLForOS(tc.p, tc.os); got != tc.want {
			t.Fatalf("%q: %q", tc.p, got)
		}
	}
}
