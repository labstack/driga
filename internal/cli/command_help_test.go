package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpOnlyAdvertisesSupportedJSON(t *testing.T) {
	for _, tc := range []struct {
		args []string
		json bool
	}{
		{[]string{"top"}, false}, {[]string{"service"}, false}, {[]string{"hooks", "status"}, false},
		{[]string{"agents", "list"}, true}, {[]string{"config", "path"}, true}, {[]string{"config", "show"}, false},
	} {
		var out, diagnostic bytes.Buffer
		code := run(append(tc.args, "--help"), "test", nil, &out, &diagnostic)
		if code != 0 || strings.Contains(out.String(), "--json") != tc.json {
			t.Fatalf("%v: %d %s %s", tc.args, code, &out, &diagnostic)
		}
		if tc.args[0] == "top" && !strings.Contains(strings.Join(strings.Fields(out.String()), " "), "Initial tab: flow, projects or machines.") {
			t.Fatal(out.String())
		}
	}
}
