package variant

import "testing"

func TestSuffix(t *testing.T) {
	for exe, want := range map[string]string{"/opt/driga": "", "/opt/drigad": "", "/opt/driga-dev": "-dev", "/opt/drigad-dev": "-dev", "/opt/driga.exe": "", "/opt/service.test": "", "/opt/drigadx": ""} {
		if got := Of(exe); got != want {
			t.Errorf("Of(%q) = %q, want %q", exe, got, want)
		}
	}
	// A build under the release's name is a development build unless the
	// release marker says otherwise; a variant's name always wins.
	for _, tc := range []struct {
		exe     string
		release bool
		want    string
	}{
		{"/opt/driga", true, ""}, {"/opt/driga", false, "-dev"}, {"/opt/drigad", false, "-dev"},
		{"/opt/driga-dev", true, "-dev"}, {"/opt/driga-foo", false, "-foo"}, {"", false, "-dev"},
	} {
		if got := For(tc.exe, tc.release); got != tc.want {
			t.Errorf("For(%q, %t) = %q, want %q", tc.exe, tc.release, got, tc.want)
		}
	}
	if got := Suffix(); got != "-dev" {
		t.Errorf("a test binary is a development build; Suffix = %q", got)
	}
	restore := SetForTest("")
	if got := Suffix(); got != "" {
		t.Errorf("SetForTest: Suffix = %q", got)
	}
	restore()
	if got := Suffix(); got != "-dev" {
		t.Errorf("restore: Suffix = %q", got)
	}
}
