package cli

import "testing"

func TestGUIExecutable(t *testing.T) {
	for _, tt := range []struct{ exe, want string }{
		{`C:\Program Files\Driga\bin\driga.exe`, `C:\Program Files\Driga\Driga.exe`},
		{`C:\Program Files\Driga\BIN\driga.exe`, `C:\Program Files\Driga\Driga.exe`},
		{`C:\src\driga\driga.exe`, ""},
	} {
		if got := guiExecutable(tt.exe); got != tt.want {
			t.Errorf("guiExecutable(%q) = %q, want %q", tt.exe, got, tt.want)
		}
	}
}
