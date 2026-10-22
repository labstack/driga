package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestTopRequiresInteractiveTerminal(t *testing.T) {
	readEnv(t)
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	for _, input := range []io.Reader{null, strings.NewReader("")} {
		var out, diagnostic bytes.Buffer
		code := run([]string{"top"}, "test", input, &out, &diagnostic)
		if code != 2 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "driga top needs an interactive terminal; use driga agents or --json") {
			t.Fatalf("%d: %s", code, &diagnostic)
		}
	}
}

func TestMissingSessionIDs(t *testing.T) {
	readEnv(t)
	readDB(t, func(*store.Tx) error { return nil })
	for _, cmd := range []string{"agent", "flow"} {
		for _, jsonMode := range []bool{false, true} {
			args := []string{cmd, "999999999"}
			if jsonMode {
				args = append(args, "--json")
			}
			var out, diagnostic bytes.Buffer
			code := run(args, "test", nil, &out, &diagnostic)
			if code != 1 || !strings.Contains(diagnostic.String(), "agent 999999999 not found; list ids with driga agents") {
				t.Fatalf("%v: %d %s %s", args, code, &out, &diagnostic)
			}
			if jsonMode && !strings.Contains(out.String(), `"code":"not_found"`) {
				t.Fatal(out.String())
			}
		}
	}
}

func TestHelpOnlyAdvertisesSupportedJSON(t *testing.T) {
	for _, tc := range []struct {
		args []string
		json bool
	}{
		{[]string{"top"}, false}, {[]string{"service"}, false}, {[]string{"hooks", "status"}, false},
		{[]string{"agents"}, true}, {[]string{"config", "path"}, true}, {[]string{"config", "show"}, false},
	} {
		var out, diagnostic bytes.Buffer
		code := run(append(tc.args, "--help"), "test", nil, &out, &diagnostic)
		if code != 0 || strings.Contains(out.String(), "--json") != tc.json {
			t.Fatalf("%v: %d %s %s", tc.args, code, &out, &diagnostic)
		}
		if tc.args[0] == "top" && !strings.Contains(out.String(), "Initial tab: home or machine.") {
			t.Fatal(out.String())
		}
	}
}

func TestUsageDocumentsTabsAndInboxConfig(t *testing.T) {
	b, err := os.ReadFile("../../docs/usage.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"`0`–`8`", "Flow, Chat", "$XDG_CONFIG_HOME/driga/config.toml", "[inbox]\nalerts = \"off\"", "`inbox.alert_after`", "`inbox.alert_repeat`"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("usage missing %s", want)
		}
	}
}

func TestConfigShowMissingExplicitPath(t *testing.T) {
	readEnv(t)
	if err := os.Remove(os.Getenv("DRIGA_CONFIG")); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	code := run([]string{"config", "show"}, "test", nil, &out, &diagnostic)
	if code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), os.Getenv("DRIGA_CONFIG")) {
		t.Fatalf("%d %s %s", code, &out, &diagnostic)
	}
}
