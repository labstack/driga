package service

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestServiceConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := Config("/opt/driga")
	if c.Name != "run.driga.collector" || c.DisplayName != "driga collector" || c.Executable != "/opt/driga" || !reflect.DeepEqual(c.Arguments, []string{"collect"}) {
		t.Fatalf("unexpected config: %+v", c)
	}
	for _, key := range []string{"UserService", "KeepAlive", "RunAtLoad"} {
		if c.Option[key] != true {
			t.Errorf("%s = %v", key, c.Option[key])
		}
	}
	// Only inspect configuration: never construct/control a real service here.
	template, ok := c.Option["LaunchdConfig"].(string)
	if !ok || strings.Count(template, filepath.Join(home, "Library", "Logs", "driga", "collector.log")) != 2 {
		t.Fatalf("launchd must send stdout and stderr to collector.log: %s", template)
	}
}
