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
	"github.com/pelletier/go-toml/v2/unstable"
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
// Existing notify settings remain untouched.
func CodexInstall(configPath, driga string, dryRun bool, now time.Time) (Diff, error) {
	d, _, err := readCodex(configPath)
	if err != nil {
		return d, err
	}
	if !filepath.IsAbs(driga) {
		return d, fmt.Errorf("driga binary path must be absolute: %q", driga)
	}
	if strings.Contains(d.Before, "# driga needs-you begin") {
		// Reuse uninstall's strict ownership validation before editing the block.
		removal, err := CodexUninstall(configPath, true)
		if err != nil {
			return d, err
		}
		pos := strings.Index(d.Before, "\n# driga needs-you begin\n")
		if !removal.Changed || pos < 0 {
			return d, fmt.Errorf("Driga permission hook changed; refusing to rewrite Codex config")
		}
		command := codexPermissionCommand(d.Before[pos:])
		program, _, _ := hookProgram(command)
		if !sameBinary(program, driga) {
			updated, err := hookCommand(driga, "codex", "PermissionRequest", runtime.GOOS)
			if err != nil {
				return d, err
			}
			block, err := editCodexString(d.Before[pos:], "command", updated)
			if err != nil {
				return d, err
			}
			d.After = d.Before[:pos] + block
		}
		d.Changed = d.Before != d.After
		return d, writeDiff(d, dryRun, now)
	}
	command, err := hookCommand(driga, "codex", "PermissionRequest", runtime.GOOS)
	if err != nil {
		return d, err
	}
	block := "\n# driga needs-you begin\n[[hooks.PermissionRequest]]\n[[hooks.PermissionRequest.hooks]]\ntype = 'command'\ncommand = " + strconv.Quote(command) + "\ntimeout = 3\n# driga needs-you end\n"
	d.After = d.Before + block
	var check map[string]any
	if err := toml.Unmarshal([]byte(d.After), &check); err != nil {
		return d, fmt.Errorf("Codex inline hooks cannot be safely appended: %w", err)
	}
	d.Changed = true
	return d, writeDiff(d, dryRun, now)
}

// CodexUninstall removes only the owned permission block. Changed blocks are refused.
func CodexUninstall(configPath string, dryRun bool) (Diff, error) {
	d, _, err := readCodex(configPath)
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
	return d, nil
}

// editCodexString changes a parsed string span, preserving comments, quoting of
// keys, multiline arrays, and all bytes outside that value.
func editCodexString(src, key, value string) (string, error) {
	var parser unstable.Parser
	parser.Reset([]byte(src))
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.KeyValue {
			continue
		}
		keys := node.Key()
		if !keys.Next() || string(keys.Node().Data) != key || !keys.IsLast() {
			continue
		}
		val := node.Value()
		if val == nil || val.Kind != unstable.String {
			break
		}
		start, end := int(val.Raw.Offset), int(val.Raw.Offset+val.Raw.Length)
		return src[:start] + strconv.Quote(value) + src[end:], nil
	}
	return src, fmt.Errorf("cannot safely update Driga %s executable", key)
}

func codexPermissionCommand(block string) string {
	var owned map[string]any
	if toml.Unmarshal([]byte(block), &owned) != nil {
		return ""
	}
	h, _ := owned["hooks"].(map[string]any)
	groups, _ := h["PermissionRequest"].([]any)
	if len(groups) != 1 {
		return ""
	}
	group, _ := groups[0].(map[string]any)
	entries, _ := group["hooks"].([]any)
	if len(entries) != 1 {
		return ""
	}
	entry, _ := entries[0].(map[string]any)
	command, _ := entry["command"].(string)
	return command
}

// CodexStatus names only registrations owned by Driga, never a foreign notify.
func CodexStatus(path, driga string) (string, error) {
	d, _, err := readCodex(path)
	if err != nil {
		return "", err
	}
	removal, err := CodexUninstall(path, true)
	if err != nil {
		return "", err
	}
	if !removal.Changed {
		return "not installed", nil
	}
	detail := "PermissionRequest installed"
	pos := strings.Index(d.Before, "\n# driga needs-you begin\n")
	program, _, _ := hookProgram(codexPermissionCommand(d.Before[pos:]))
	stale := !binaryCurrent(program, driga)
	if stale {
		return "stale binary; " + detail, nil
	}
	return detail, nil
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
