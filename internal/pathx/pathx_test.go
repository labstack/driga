package pathx

import "testing"

func TestContainment(t *testing.T) {
	for _, tc := range []struct {
		os, p, root string
		want        bool
	}{
		{"linux", "/work/a", "/work", true},
		{"darwin", "/Work/a", "/work", false},
		{"linux", "/work-other", "/work", false},
		{"linux", "/work/../other", "/work", false},
		{"linux", "", "/work", false},
		{"windows", `C:\Work\子 folder\a`, `c:/work`, true},
		{"windows", `\\?\C:\Work\a`, `c:/WORK`, true},
		{"windows", `\\?\UNC\Server\Share\a`, `\\server\share`, true},
		{"windows", `\\server\other\a`, `\\server\share`, false},
		{"windows", `C:\Work-other`, `c:\work`, false},
		{"windows", `C:\Work\..\other`, `c:\work`, false},
		{"windows", `D:\Work\a`, `c:\work`, false},
		{"windows", `C:work`, `c:\work`, false},
	} {
		if got := withinForOS(tc.p, tc.root, tc.os); got != tc.want {
			t.Errorf("%s Within(%q,%q)=%v", tc.os, tc.p, tc.root, got)
		}
	}
}

func TestKeys(t *testing.T) {
	if keyForOS(`\\?\UNC\Server\Share\A`, "windows") != keyForOS(`//server/share/a`, "windows") {
		t.Fatal("UNC keys differ")
	}
	if keyForOS("/Work/a", "darwin") == keyForOS("/work/a", "darwin") {
		t.Fatal("Unix case folded")
	}
	if !Equal("a/../b", "b") || !Within("b/c", "b") || Key("") != "" {
		t.Fatal("native wrappers")
	}
}
