package feed

import (
	"bytes"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestSummaryFeedGate(t *testing.T) {
	for _, tc := range []struct {
		share            ShareText
		snippets, served bool
	}{{None, false, false}, {Titles, true, false}, {Snippets, true, true}, {Titles, false, true}, {ShareText("invalid"), false, false}} {
		row := SummaryRow{Summary: model.Summary{ProjectID: 88, Text: "summary private", UsedSnippets: tc.snippets}, RepoPath: "/fixture"}
		key, p := Encode(Summary, row, tc.share)
		if (p != nil) != tc.served {
			t.Fatalf("%+v: %s", tc, p)
		}
		if p != nil {
			if bytes.Contains(p, []byte("project_id")) {
				t.Fatal(string(p))
			}
			if key != Key("/fixture", tc.snippets) {
				t.Fatal(key)
			}
			if _, err := Decode(Summary, p); err != nil {
				t.Fatal(err)
			}
		}
	}
}
