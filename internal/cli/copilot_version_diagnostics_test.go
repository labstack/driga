package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/attention"
)

type copilotVersionRunner struct {
	output string
	err    error
}

func (r copilotVersionRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "copilot" || strings.Join(args, " ") != "--version" {
		return nil, errors.New("unexpected command")
	}
	return []byte(r.output), r.err
}
func TestCopilotVersionWarnings(t *testing.T) {
	for _, tc := range []struct {
		version string
		warn    bool
	}{
		{"1.0.97", true}, {"GitHub Copilot CLI 1.0.97.\nRun 'copilot update' to check for updates.\n", true}, {"GitHub Copilot CLI v1.0.9\n", true}, {"1.0.98", false}, {"1.1.0", false}, {"2.0.0", false}, {"nonsense", false}, {"99999999999999999999999.0.0", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			d, _ := doctorFixture(t)
			d.OS = "linux"
			old := newInboxRunner
			t.Cleanup(func() { newInboxRunner = old })
			newInboxRunner = func() attention.Runner { return copilotVersionRunner{output: tc.version} }
			checks := Doctor(context.Background(), d)
			found := false
			for _, c := range checks {
				if c.Name == "version:copilot" && c.Status == "warn" {
					found = true
					if !strings.Contains(c.Detail, "1.0.98") {
						t.Fatal(c)
					}
				}
			}
			if found != tc.warn {
				t.Fatal(tc, checks)
			}
			t.Run("hook install", func(t *testing.T) {
				var out bytes.Buffer
				if err := (HooksInstallCmd{HooksOptions{Target: "copilot", DryRun: true}}).Run(&Globals{Stdout: &out}); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(out.String(), "Warning:") != tc.warn {
					t.Fatal(tc, out.String())
				}
			})
		})
	}
}
