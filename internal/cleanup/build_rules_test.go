package cleanup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQualifyBuild(t *testing.T) {
	for _, tc := range []struct {
		rel, evidence string
		user          []string
		want          string
	}{
		{"node_modules/", "package.json", nil, ""},
		{"node_modules/", "", nil, "no package.json beside node_modules"},
		{"pkg/node_modules/", "pkg/package.json", nil, ""},
		{"out/", "package.json", nil, "not a known regenerable directory"},
		{".terraform/", "", []string{".terraform/"}, "not a known regenerable directory"},
		{".codegraph/", "", []string{".codegraph/"}, "not a known regenerable directory"},
		{"models/", "", []string{"models/"}, ""},
		{".venv/", "requirements-dev.txt", nil, ""},
		{"build/", "build.gradle.kts", nil, ""},
	} {
		t.Run(tc.rel+tc.evidence, func(t *testing.T) {
			root := t.TempDir()
			if tc.evidence != "" {
				put(t, filepath.Join(root, tc.evidence), "")
			}
			rule, err := qualifyBuild(root, tc.rel, tc.user)
			if tc.want == "" {
				if err != nil || rule == "" {
					t.Fatalf("%q %v", rule, err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func put(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
}
