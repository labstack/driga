package hooks

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func codexOTLP(endpoints ...string) map[string]any {
	return map[string]any{
		"log_user_prompt": false,
		"exporter":        map[string]any{"otlp-http": map[string]any{"endpoint": otlpEndpoint(endpoints) + "/v1/logs", "protocol": "json"}},
	}
}

// CodexInstallOTLP configures the documented logs exporter; Codex facts stay unmapped.
// The traces option applies only to Claude's documented spans.
func CodexInstallOTLP(path string, _ bool, dryRun bool, now time.Time, endpoints ...string) (Diff, error) {
	d, settings, err := readCodex(path)
	if err != nil {
		return d, err
	}
	if _, exists := settings["otel"]; exists {
		return d, ErrTelemetryInUse
	}
	settings["otel"] = codexOTLP(endpoints...)
	return finishCodex(d, settings, dryRun, now)
}

// CodexUninstallOTLP removes only the exact Driga metadata-only loopback exporter.
func CodexUninstallOTLP(path string, dryRun bool, endpoints ...string) (Diff, error) {
	d, settings, err := readCodex(path)
	if err != nil {
		return d, err
	}
	if !reflect.DeepEqual(settings["otel"], codexOTLP(endpoints...)) {
		return d, nil
	}
	delete(settings, "otel")
	return finishCodex(d, settings, dryRun, time.Now())
}

// CodexInstall appends a report-only permission hook, preserving unrelated TOML.
// It retains the legacy notify registration and refuses an existing foreign notify.
func CodexInstall(configPath, driga string, dryRun bool, now time.Time) (Diff, error) {
	d, settings, err := readCodex(configPath)
	if err != nil {
		return d, err
	}
	if !filepath.IsAbs(driga) {
		return d, fmt.Errorf("driga binary path must be absolute: %q", driga)
	}
	if strings.Contains(d.Before, "# driga needs-you begin") {
		return d, nil
	}
	// The permission hook is its own table, so any existing notify (ours or
	// another tool's, such as Codex Computer Use) is left untouched. Only an
	// unset notify gets Driga's, owned by a prefix so uninstall restores every
	// original byte.
	_, hadNotify := settings["notify"]
	notify, _ := toml.Marshal(map[string]any{"notify": []string{driga, "hook", "codex", "notify"}})
	command, err := hookCommand(driga, "codex", "PermissionRequest", runtime.GOOS)
	if err != nil {
		return d, err
	}
	block := "\n# driga needs-you begin\n[[hooks.PermissionRequest]]\n[[hooks.PermissionRequest.hooks]]\ntype = 'command'\ncommand = " + strconv.Quote(command) + "\ntimeout = 3\n# driga needs-you end\n"
	prefix := string(notify)
	if hadNotify {
		prefix = ""
	} else {
		block = strings.Replace(block, "# driga needs-you begin\n", "# driga needs-you begin\n# driga notify added\n", 1)
	}
	d.After = prefix + d.Before + block
	var check map[string]any
	if err := toml.Unmarshal([]byte(d.After), &check); err != nil {
		return d, fmt.Errorf("Codex inline hooks cannot be safely appended: %w", err)
	}
	d.Changed = true
	return d, writeDiff(d, dryRun, now)
}

// CodexUninstall round-trips the owned permission block and its optional notify
// prefix. Changed blocks are refused; legacy notify-only settings retain removal.
func CodexUninstall(configPath string, dryRun bool) (Diff, error) {
	d, settings, err := readCodex(configPath)
	if err != nil {
		return d, err
	}
	const start = "\n# driga needs-you begin\n"
	const end = "# driga needs-you end\n"
	if pos := strings.Index(d.Before, start); pos >= 0 && strings.HasSuffix(d.Before, end) {
		// Remove only our exact, unmodified report-only registration.
		var owned map[string]any
		if toml.Unmarshal([]byte(d.Before[pos:]), &owned) == nil {
			hooks, _ := owned["hooks"].(map[string]any)
			groups, _ := hooks["PermissionRequest"].([]any)
			if len(groups) == 1 {
				g, _ := groups[0].(map[string]any)
				hs, _ := g["hooks"].([]any)
				if len(hs) == 1 {
					h, _ := hs[0].(map[string]any)
					cmd, _ := h["command"].(string)
					p, rest, ok := hookProgram(cmd)
					if ok && ours(p, currentBinary()) && rest == " hook codex PermissionRequest" && h["type"] == "command" && h["timeout"] == int64(3) && len(h) == 3 && len(g) == 1 {
						after := d.Before[:pos]
						if strings.Contains(d.Before[pos:], "# driga notify added\n") && codexNotifyOurs(settings["notify"], currentBinary()) {
							line, _ := toml.Marshal(map[string]any{"notify": settings["notify"]})
							after = strings.TrimPrefix(after, string(line))
						}
						d.After, d.Changed = after, true
						return d, writeDiff(d, dryRun, time.Now())
					}
				}
			}
		}
	}
	if strings.Contains(d.Before, "# driga needs-you begin") {
		return d, fmt.Errorf("Driga permission hook changed; refusing to rewrite Codex config")
	}
	if !codexNotifyOurs(settings["notify"], currentBinary()) {
		return d, nil
	}
	delete(settings, "notify")
	return finishCodex(d, settings, dryRun, time.Now())
}

func codexNotifyOurs(value any, driga string) bool {
	args, ok := value.([]any)
	if !ok || len(args) != 4 {
		return false
	}
	program, ok := args[0].(string)
	return ok && ours(program, driga) && args[1] == "hook" && args[2] == "codex" && args[3] == "notify"
}

func readCodex(path string) (Diff, map[string]any, error) {
	d, err := readDiff(path)
	if err != nil {
		return d, nil, err
	}
	settings := make(map[string]any)
	if err := toml.Unmarshal([]byte(d.Before), &settings); err != nil {
		return d, nil, fmt.Errorf("read Codex config %s: %w", path, err)
	}
	return d, settings, nil
}

func finishCodex(d Diff, settings map[string]any, dryRun bool, now time.Time) (Diff, error) {
	b, err := toml.Marshal(settings)
	if err != nil {
		return d, err
	}
	d.After, d.Changed = string(b), true
	d.FullRewrite = true
	return d, writeDiff(d, dryRun, now)
}
