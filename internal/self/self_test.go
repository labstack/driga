package self

import "testing"

func TestCLI(t *testing.T) {
	for in, want := range map[string]string{
		"/Applications/Driga.app/Contents/MacOS/drigad": "/Applications/Driga.app/Contents/MacOS/driga",
		"/home/me/.local/libexec/drigad-dev":            "/home/me/.local/libexec/driga-dev",
		"/opt/drigad.exe":                               "/opt/driga.exe",
		"/opt/driga":                                    "/opt/driga",
		"/opt/driga-dev":                                "/opt/driga-dev",
		"/opt/drigadx":                                  "/opt/drigadx",
	} {
		if got := CLI(in); got != want {
			t.Errorf("CLI(%q) = %q, want %q", in, got, want)
		}
	}
}
