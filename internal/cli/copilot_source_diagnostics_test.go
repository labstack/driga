package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestCopilotSandboxAndNoEventsWarnings(t *testing.T) {
	d, st := doctorFixture(t)
	d.OS = "linux"
	d.CopilotHome = filepath.Join(d.Home, "custom-copilot")
	if err := os.MkdirAll(d.CopilotHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{{PID: 123, Cmdline: "copilot", Argv: []string{"copilot"}}})
	}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"sandbox":{"enabled":true},"other":{"enabled":"not bool"}}`, `{"sandbox":{"enabled":false}}`, `{`, ""} {
		if err := os.WriteFile(filepath.Join(d.CopilotHome, "settings.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		checks := Doctor(context.Background(), d)
		sandbox := false
		for _, c := range checks {
			if c.Name == "sandbox:copilot" && c.Status == "warn" {
				sandbox = true
			}
		}
		if sandbox != strings.Contains(body, `"enabled":true`) {
			t.Fatal(body, checks)
		}
		requireCheck(t, checks, "events:copilot", "warn", "")
	}
	if err := st.SetMeta("hooks:copilot:received", "1"); err != nil {
		t.Fatal(err)
	}
	for _, c := range Doctor(context.Background(), d) {
		if c.Name == "events:copilot" && c.Status == "warn" {
			t.Fatal(c)
		}
	}
}
