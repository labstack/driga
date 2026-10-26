package cli

import (
	"os"
	"strings"
	"testing"
)

func TestReviewedDocumentationMatchesService(t *testing.T) {
	for _, tc := range []struct{ path, want, wrong string }{
		{"site/src/content/docs/start/first-run.mdx", "Quit leaves the service running", "as its own child"},
		{"site/src/content/docs/start/first-run.mdx", "journalctl --user -u driga", "journalctl --user -u run.driga.service"},
		{"site/src/content/docs/start/install.mdx", `\Driga\<SID>\Driga collector`, "removes the Driga service task installed earlier"},
		{"docs/machine-health.md", "warning click opens the app's Fleet page", "click opens Terminal"},
		{"site/src/content/docs/guides/notifications.mdx", "service posts alerts whether or not the app is open", "Alerts come from the Driga app"},
		{"CONTRIBUTING.md", "review convention covers every component", "conventions/design.md"},
		{"docs/usage.md", "Driga service (dev)", "The\nthe Driga service"},
	} {
		b, err := os.ReadFile("../../" + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), tc.want) || strings.Contains(string(b), tc.wrong) {
			t.Errorf("%s contract drift", tc.path)
		}
	}
}
