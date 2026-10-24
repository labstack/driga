package hooks

import (
	"reflect"
	"strings"
	"testing"
)

func TestWindowsHookCommand(t *testing.T) {
	path := `C:\Tools with spaces\界\driga.exe`
	got, err := hookEntry(path, "claude", "Stop", "windows")
	if err != nil || got["command"] != path || !reflect.DeepEqual(got["args"], []string{"hook", "claude", "Stop"}) {
		t.Fatal(got, err)
	}
	// Exec form must preserve characters that shells would expand.
	for _, path := range []string{`C:\A&B\driga.exe`, `\\server\a $b\driga.exe`} {
		got, err := hookEntry(path, "claude", "Stop", "windows")
		if err != nil || got["command"] != path {
			t.Fatal(got, err)
		}
	}
	cmd, err := hookCommand(path, "codex", "PermissionRequest", "windows")
	if err != nil || cmd != `cmd.exe /d /c call "C:\Tools with spaces\界\driga.exe" hook codex PermissionRequest` {
		t.Fatal(cmd, err)
	}
	for _, path := range []string{`C:\a%PATH%\driga.exe`, `C:\a&b\driga.exe`, "C:\\a\n\\driga.exe", `driga.exe`} {
		if _, err := hookCommand(path, "codex", "PermissionRequest", "windows"); err == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
	unix, err := hookCommand("/opt/a user's tools/driga", "claude", "Stop", "linux")
	if err != nil || !strings.HasPrefix(unix, `'/opt/a user'"'"'s tools/driga' hook`) {
		t.Fatal(unix, err)
	}
}

func TestClaudeHookOwnershipRequiresCompleteInvocation(t *testing.T) {
	for _, command := range []string{"/opt/driga-dev hook codex Stop", "/opt/driga-dev hook claude Stop extra", "/opt/driga-dev hook claude Stop; echo retained", "/opt/driga-dev hook claude Stop && echo retained", "/opt/drigax hook claude Stop"} {
		if isClaudeHook(map[string]any{"command": command}, "/opt/driga") {
			t.Errorf("foreign command recognized: %s", command)
		}
	}
	for _, command := range []string{"/opt/driga-dev hook claude Stop", "'/opt/old tools/driga-custom.exe' hook claude Stop"} {
		if !isClaudeHook(map[string]any{"command": command}, "/opt/driga") {
			t.Errorf("build not recognized: %s", command)
		}
	}
}
