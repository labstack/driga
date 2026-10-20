package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

var copilotVersionPattern = regexp.MustCompile(`(?im)^(?:GitHub Copilot(?: CLI)?[: ]+)?v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:[-+][a-z0-9.-]+)?\.?[ \t\r]*$`)

// Version probing uses the same injected, bounded runner as other agent CLI
// reads. Unknown output and missing binaries never prevent hook installation.
func copilotVersionWarning(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	b, err := newInboxRunner().Run(ctx, "copilot", "--version")
	if err != nil {
		return ""
	}
	m := copilotVersionPattern.FindSubmatch(b)
	if m == nil {
		return ""
	}
	var version [3]uint64
	for i := range version {
		n, err := strconv.ParseUint(string(m[i+1]), 10, 64)
		if err != nil {
			return ""
		}
		version[i] = n
	}
	if version[0] > 1 || version[0] == 1 && (version[1] > 0 || version[2] >= 98) {
		return ""
	}
	return "Copilot CLI 1.0.98 or newer is required; older versions can hang hooks until timeout. Upgrade Copilot and restart sessions."
}

func copilotSandboxEnabled(home, copilotHome string) bool {
	if copilotHome == "" {
		copilotHome = filepath.Join(home, ".copilot")
	}
	b, err := os.ReadFile(filepath.Join(copilotHome, "settings.json"))
	if err != nil {
		return false
	}
	// Decode only this key; other settings have no bearing on spool access.
	var settings struct {
		Sandbox struct {
			Enabled bool `json:"enabled"`
		} `json:"sandbox"`
	}
	return json.Unmarshal(b, &settings) == nil && settings.Sandbox.Enabled
}
