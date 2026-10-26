package scripts_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestGeneratedJSONCheckoutUsesLF(t *testing.T) {
	// Exercise Git's checkout conversion, including on Unix hosts, rather
	// than relying on the runner's core.autocrlf setting.
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "core.autocrlf", "true")
	writeFixtureFile(t, filepath.Join(repo, ".gitattributes"), readRepositoryFile(t, "../.gitattributes"), 0600)
	for _, path := range []string{"ui/src/fixtures/fragments.json", "ui/src/theme/palette.json"} {
		full := filepath.Join(repo, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		writeFixtureFile(t, full, "{\n \"fixture\": true\n}\n", 0600)
		runGit(t, repo, "add", path)
		if err := os.Remove(full); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "checkout-index", "--", path)
		got, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(got, []byte("\r\n")) {
			t.Errorf("%s checked out with CRLF; generated fixture checks require LF", path)
		}
	}
}

func TestChromiumInstallHasStepTimeout(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name    string
				Run     string
				Timeout int `yaml:"timeout-minutes"`
			}
		}
	}
	if err := yaml.Unmarshal([]byte(readRepositoryFile(t, "../.github/workflows/ci.yml")), &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["test"].Steps {
		if step.Name != "Install Chromium for the catalog check" {
			continue
		}
		if step.Timeout <= 0 || step.Timeout > 10 {
			t.Errorf("Chromium installation needs a step timeout of at most 10 minutes; got %d", step.Timeout)
		}
		if !strings.Contains(step.Run, "bun x playwright install --with-deps chromium") {
			t.Error("Chromium installation must include Playwright's system dependencies")
		}
		return
	}
	t.Fatal("Chromium installation step missing")
}
