package cli

import (
	"os"
	"strings"
	"testing"
)

func TestUsageDocumentsTabsAndInboxConfig(t *testing.T) {
	b, err := os.ReadFile("../../docs/usage.md")
	if err != nil {
		t.Fatal(err)
	}
	// Windows checkouts may convert the docs to CRLF line endings.
	doc := strings.ReplaceAll(string(b), "\r\n", "\n")
	for _, want := range []string{"`0 Flow`", "`1 Projects`", "`2 Machines`", "`c` opens contextual Chat", "$XDG_CONFIG_HOME/driga/config.toml", "[inbox]\nalerts = \"off\"", "`inbox.alert_after`", "`inbox.alert_repeat`"} {
		if !strings.Contains(doc, want) {
			t.Errorf("usage missing %s", want)
		}
	}
}
