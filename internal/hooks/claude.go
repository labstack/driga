// Package hooks installs and removes reversible agent hook settings.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Diff contains the complete original and proposed file contents.
type Diff struct {
	Path, Before, After string
	Changed             bool
}

// Unified renders a simple line diff, omitting common leading and trailing lines.
func (d Diff) Unified() string {
	if !d.Changed || d.Before == d.After {
		return ""
	}
	lines := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	before, after := lines(d.Before), lines(d.After)
	for len(before) > 0 && len(after) > 0 && before[0] == after[0] {
		before, after = before[1:], after[1:]
	}
	for len(before) > 0 && len(after) > 0 && before[len(before)-1] == after[len(after)-1] {
		before, after = before[:len(before)-1], after[:len(after)-1]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", d.Path, d.Path)
	for _, line := range before {
		fmt.Fprintf(&b, "-%s\n", line)
	}
	for _, line := range after {
		fmt.Fprintf(&b, "+%s\n", line)
	}
	return b.String()
}

var claudeEvents = []string{"Stop", "Notification", "SessionStart", "SessionEnd"}

// ErrTelemetryInUse prevents replacing any existing agent telemetry destination.
var ErrTelemetryInUse = errors.New("telemetry is already configured; Driga cannot replace or relay it")

func claudeOTLPEnv(traces bool) map[string]string {
	traceExporter := "none"
	if traces {
		traceExporter = "otlp"
	}
	return map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
		"OTEL_LOGS_EXPORTER":           "otlp", "OTEL_METRICS_EXPORTER": "none",
		"OTEL_TRACES_EXPORTER":        traceExporter,
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/json",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:7418",
	}
}

// ClaudeInstallOTLP installs metadata-only telemetry without replacing telemetry settings.
func ClaudeInstallOTLP(path string, traces, dryRun bool, now time.Time) (Diff, error) {
	d, settings, err := readClaude(path)
	if err != nil {
		return d, err
	}
	env := make(map[string]any)
	if v, exists := settings["env"]; exists {
		var ok bool
		env, ok = v.(map[string]any)
		if !ok {
			return d, fmt.Errorf("Claude env must be an object")
		}
	}
	for key := range env {
		if strings.HasPrefix(key, "OTEL_") || strings.HasPrefix(key, "CLAUDE_CODE_ENABLE_TELEMETRY") || strings.Contains(key, "TRACING") {
			return d, ErrTelemetryInUse
		}
	}
	for k, v := range claudeOTLPEnv(traces) {
		env[k] = v
	}
	settings["env"] = env
	d.Changed = true
	return finishClaude(d, settings, dryRun, now)
}

// ClaudeUninstallOTLP removes only an intact Driga loopback telemetry configuration.
func ClaudeUninstallOTLP(path string, dryRun bool) (Diff, error) {
	d, settings, err := readClaude(path)
	if err != nil {
		return d, err
	}
	env, ok := settings["env"].(map[string]any)
	if !ok {
		return d, nil
	}
	for _, traces := range []bool{false, true} {
		want := claudeOTLPEnv(traces)
		match := true
		for k, v := range want {
			if env[k] != v {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		for k := range want {
			delete(env, k)
		}
		if len(env) == 0 {
			delete(settings, "env")
		}
		d.Changed = true
		return finishClaude(d, settings, dryRun, time.Now())
	}
	return d, nil
}

// ClaudeInstall merges missing Driga events into Claude's settings.
func ClaudeInstall(settingsPath, driga string, dryRun bool, now time.Time) (Diff, error) {
	d, settings, err := readClaude(settingsPath)
	if err != nil {
		return d, err
	}
	if !filepath.IsAbs(driga) {
		return d, fmt.Errorf("driga binary path must be absolute: %q", driga)
	}
	h, err := claudeHooks(settings)
	if err != nil {
		return d, err
	}
	for _, event := range claudeEvents {
		var groups []any
		if value, exists := h[event]; exists {
			var ok bool
			groups, ok = value.([]any)
			if !ok {
				return d, fmt.Errorf("hooks.%s must be an array", event)
			}
		}
		if hasClaudeHook(groups, driga) {
			continue
		}
		h[event] = append(groups, map[string]any{
			"matcher": "",
			"hooks": []any{map[string]any{
				"type": "command", "command": shellQuote(driga) + " hook claude " + event,
			}},
		})
		d.Changed = true
	}
	if !d.Changed {
		return d, nil
	}
	settings["hooks"] = h
	return finishClaude(d, settings, dryRun, now)
}

// ClaudeUninstall removes Driga entries, pruning the groups they leave empty.
func ClaudeUninstall(settingsPath string, dryRun bool) (Diff, error) {
	d, settings, err := readClaude(settingsPath)
	if err != nil {
		return d, err
	}
	h, err := claudeHooks(settings)
	if err != nil {
		return d, err
	}
	driga := currentBinary()
	for event, value := range h {
		groups, ok := value.([]any)
		if !ok {
			continue
		}
		keptGroups := make([]any, 0, len(groups))
		eventChanged := false
		for _, value := range groups {
			group, ok := value.(map[string]any)
			if !ok {
				keptGroups = append(keptGroups, value)
				continue
			}
			entries, ok := group["hooks"].([]any)
			if !ok {
				keptGroups = append(keptGroups, value)
				continue
			}
			kept := make([]any, 0, len(entries))
			removed := false
			for _, entry := range entries {
				if isClaudeHook(entry, driga) {
					removed, eventChanged = true, true
				} else {
					kept = append(kept, entry)
				}
			}
			if !removed {
				keptGroups = append(keptGroups, value)
			} else if len(kept) > 0 {
				group["hooks"] = kept
				keptGroups = append(keptGroups, group)
			}
		}
		if eventChanged {
			d.Changed = true
			if len(keptGroups) == 0 {
				delete(h, event)
			} else {
				h[event] = keptGroups
			}
		}
	}
	if !d.Changed {
		return d, nil
	}
	if len(h) == 0 {
		delete(settings, "hooks")
	}
	return finishClaude(d, settings, dryRun, time.Now())
}

// ClaudeStatus returns sorted event names containing a Driga hook.
func ClaudeStatus(settingsPath string) (installed []string, err error) {
	_, settings, err := readClaude(settingsPath)
	if err != nil {
		return nil, err
	}
	h, err := claudeHooks(settings)
	if err != nil {
		return nil, err
	}
	driga := currentBinary()
	for event, value := range h {
		if groups, ok := value.([]any); ok && hasClaudeHook(groups, driga) {
			installed = append(installed, event)
		}
	}
	sort.Strings(installed)
	return installed, nil
}

func readClaude(path string) (Diff, map[string]any, error) {
	d, err := readDiff(path)
	if err != nil {
		return d, nil, err
	}
	settings := make(map[string]any)
	if d.Before == "" {
		// A missing file starts empty; an existing empty JSON file is invalid.
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return d, settings, nil
		}
	}
	decoder := json.NewDecoder(strings.NewReader(d.Before))
	decoder.UseNumber()
	if err := decoder.Decode(&settings); err != nil {
		return d, nil, fmt.Errorf("read Claude settings %s: %w", path, err)
	}
	if settings == nil {
		return d, nil, fmt.Errorf("Claude settings %s must be an object", path)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return d, nil, fmt.Errorf("Claude settings %s contain trailing data", path)
	}
	return d, settings, nil
}

func claudeHooks(settings map[string]any) (map[string]any, error) {
	if value, ok := settings["hooks"]; ok {
		h, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Claude hooks must be an object")
		}
		return h, nil
	}
	return make(map[string]any), nil
}

func finishClaude(d Diff, settings map[string]any, dryRun bool, now time.Time) (Diff, error) {
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return d, err
	}
	d.After = string(b) + "\n"
	return d, writeDiff(d, dryRun, now)
}

func hasClaudeHook(groups []any, driga string) bool {
	for _, value := range groups {
		group, _ := value.(map[string]any)
		entries, _ := group["hooks"].([]any)
		for _, entry := range entries {
			if isClaudeHook(entry, driga) {
				return true
			}
		}
	}
	return false
}

func isClaudeHook(value any, driga string) bool {
	entry, _ := value.(map[string]any)
	command, _ := entry["command"].(string)
	program, rest, ok := shellProgram(command)
	return ok && ours(program, driga) && strings.HasPrefix(rest, " hook ")
}

func ours(program, driga string) bool {
	return program != "" && ((driga != "" && program == driga) || filepath.Base(program) == "driga")
}

func currentBinary() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}

func shellQuote(s string) string {
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_-.:@", r))
	}) < 0 && s != "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// shellProgram reads only the first shell word; it never executes commands.
func shellProgram(command string) (program, rest string, ok bool) {
	var b strings.Builder
	var quote rune
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote == 0 && unicode.IsSpace(r) {
			return b.String(), string(runes[i:]), b.Len() > 0
		}
		if r == '\\' && quote != '\'' {
			i++
			if i >= len(runes) {
				return "", "", false
			}
			b.WriteRune(runes[i])
		} else if quote != 0 && r == quote {
			quote = 0
		} else if quote == 0 && (r == '\'' || r == '"') {
			quote = r
		} else {
			b.WriteRune(r)
		}
	}
	return b.String(), "", quote == 0 && b.Len() > 0
}

func readDiff(path string) (Diff, error) {
	d := Diff{Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	d.Before, d.After = string(b), string(b)
	return d, nil
}

func writeDiff(d Diff, dryRun bool, now time.Time) error {
	if dryRun || !d.Changed {
		return nil
	}
	info, err := os.Stat(d.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	mode := os.FileMode(0600)
	if exists {
		mode = info.Mode().Perm()
		// Keep each original, even when two writes fall in the same second.
		for stamp := now.Unix(); ; stamp++ {
			backup := fmt.Sprintf("%s.driga-bak-%d", d.Path, stamp)
			f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if err != nil {
				return err
			}
			_, writeErr := io.WriteString(f, d.Before)
			closeErr := f.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				return err
			}
			break
		}
	} else if err := os.MkdirAll(filepath.Dir(d.Path), 0700); err != nil {
		return err
	}
	return os.WriteFile(d.Path, []byte(d.After), mode)
}
