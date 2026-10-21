package pathx

import "testing"

func TestRecordedJoin(t *testing.T) {
	for _, tc := range []struct{ cwd, file, want string }{
		{"/r", "/r/a.go", "/r/a.go"},
		{"/r", "/r/../a.go", "/r/../a.go"}, // Absolute paths stay exactly as recorded.
		{"/r", "dir/../a.go", "/r/a.go"},
		{"/r", `a\b.go`, `/r/a\b.go`}, // Backslashes are legal Unix filename bytes.
		{`C:\Work\子 folder`, "dir/a.go", `C:\Work\子 folder\dir\a.go`},
		{`D:/Work`, `dir\a.go`, "D:/Work/dir/a.go"},
		{`C:\Work`, `..\a.go`, `C:\a.go`},
		{`C:\`, "a.go", `C:\a.go`},
		{`\\Server\Share\Work`, `..\a.go`, `\\Server\Share\a.go`},
		{`\\Server\Share`, "../../a.go", `\\Server\Share\a.go`},
		{`C:\Work`, "/r/a.go", "/r/a.go"},
		{"/r", `D:\Agent\A.go`, `D:\Agent\A.go`},
		{"/r", `\\?\C:\Agent\A.go`, `\\?\C:\Agent\A.go`},
		{"", "a.go", "a.go"},
	} {
		t.Run(tc.cwd+"/"+tc.file, func(t *testing.T) {
			if got := RecordedJoin(tc.cwd, tc.file); got != tc.want {
				t.Fatalf("RecordedJoin(%q, %q) = %q, want %q", tc.cwd, tc.file, got, tc.want)
			}
		})
	}
}

func TestRecordedIdentity(t *testing.T) {
	for _, tc := range []struct {
		p, root string
		want    bool
	}{
		{"/Work/a", "/work", false},
		{"/Work/a", "/Work", true},
		{`/Work/a\b`, "/Work", true},
		{"/work-other", "/work", false},
		{`C:\WORK\a`, "c:/work", true},
		{`\\?\UNC\Server\Share\a`, "//server/share", true},
		{`D:\Work\a`, `C:\Work`, false},
		{`C:work`, `C:\Work`, false},
		{"/Work/a", `C:\Work`, false},
	} {
		if got := RecordedWithin(tc.p, tc.root); got != tc.want {
			t.Errorf("RecordedWithin(%q, %q) = %v", tc.p, tc.root, got)
		}
	}
	for _, tc := range []struct{ root, p, want string }{
		{"/work", "/work/Src/A.go", "Src/A.go"},
		{`C:\Work`, "c:/work/Src/A.go", "Src/A.go"},
		{`C:\K`, "c:/k/Src/A.go", "Src/A.go"},
		{`\\?\UNC\Server\Share`, "//server/share/Src/A.go", "Src/A.go"},
		{"/", "/work/a.go", "work/a.go"},
	} {
		if got, err := RecordedRel(tc.root, tc.p); err != nil || got != tc.want {
			t.Errorf("RecordedRel(%q, %q) = %q, %v", tc.root, tc.p, got, err)
		}
	}
	if RecordedEqual("/Work", "/work") || !RecordedEqual(`C:\Work`, "c:/work") {
		t.Fatal("recorded case sensitivity depends on the reader host")
	}
}

func TestRecordedTemporary(t *testing.T) {
	for _, tc := range []struct {
		cwd, root string
		want      bool
	}{
		{"/tmp/x/../y", `D:\Temp`, true},
		{"/private/tmp/x", `D:\Temp`, true},
		{"/var/tmp/x", `D:\Temp`, true},
		{"/tmp-other/x", `D:\Temp`, false},
		{"/work", `D:\Temp`, false},
		{`D:\TEMP\child`, `d:\temp`, true},
		{`D:/Temp/child`, `d:\temp`, true},
		{`D:\Temp-other\child`, `D:\Temp`, false},
		{`C:\Temp\child`, `D:\Temp`, false},
		{`D:\tmp\child`, "/tmp", false},
		{`\\Server\Share\Temp\child`, `\\server\share\temp`, true},
		{"relative", "/tmp", false},
	} {
		if got := temporaryRecorded(tc.cwd, tc.root); got != tc.want {
			t.Errorf("temporaryRecorded(%q, %q) = %v", tc.cwd, tc.root, got)
		}
	}
}
