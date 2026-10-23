// Package hooks installs and removes reversible agent hook settings.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/textfmt"

	"github.com/labstack/driga/internal/pathx"
)

// Diff contains the complete original and proposed file contents.
type Diff struct {
	Path, Before, After string
	Changed             bool
	FullRewrite         bool
	existed             bool
	target              string
	owned               bool // A standalone Driga file: no backups; empty After deletes it.
}

var claudeEvents = []string{"Stop", "Notification", "SessionStart", "SessionEnd"}

// ErrTelemetryInUse prevents replacing any existing agent telemetry destination.
var ErrTelemetryInUse = errors.New("telemetry is already configured; Driga cannot replace or relay it")

func claudeOTLPEnv(traces bool, endpoints ...string) map[string]string {
	traceExporter := "none"
	if traces {
		traceExporter = "otlp"
	}
	return map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
		"OTEL_LOGS_EXPORTER":           "otlp", "OTEL_METRICS_EXPORTER": "none",
		"OTEL_TRACES_EXPORTER":        traceExporter,
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/json",
		"OTEL_EXPORTER_OTLP_ENDPOINT": otlpEndpoint(endpoints),
	}
}

// ClaudeInstallOTLP installs metadata-only telemetry without replacing telemetry settings.
func ClaudeInstallOTLP(path string, traces, dryRun bool, now time.Time, endpoints ...string) (Diff, error) {
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
	if traces {
		if v, exists := env["CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"]; exists && v != "1" {
			return d, ErrTelemetryInUse
		}
		if _, exists := env["DRIGA_OTLP_TRACES_ADDED"]; exists {
			return d, ErrTelemetryInUse
		}
		if _, exists := env["CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"]; !exists {
			env["CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"] = "1"
			env["DRIGA_OTLP_TRACES_ADDED"] = "1"
		}
	}
	for k, v := range claudeOTLPEnv(traces, endpoints...) {
		env[k] = v
	}
	settings["env"] = env
	d.Changed = true
	return finishClaude(d, settings, dryRun, now)
}

// ClaudeUninstallOTLP removes only an intact Driga loopback telemetry configuration.
func ClaudeUninstallOTLP(path string, dryRun bool, endpoints ...string) (Diff, error) {
	d, settings, err := readClaude(path)
	if err != nil {
		return d, err
	}
	env, ok := settings["env"].(map[string]any)
	if !ok {
		return d, nil
	}
	for _, traces := range []bool{false, true} {
		want := claudeOTLPEnv(traces, endpoints...)
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
		if env["DRIGA_OTLP_TRACES_ADDED"] == "1" {
			if env["CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"] == "1" {
				delete(env, "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA")
			}
			delete(env, "DRIGA_OTLP_TRACES_ADDED")
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
	// Ownership permits repair, but does not mean the executable is current.
	for _, value := range h {
		groups, _ := value.([]any)
		for _, value := range groups {
			group, _ := value.(map[string]any)
			entries, _ := group["hooks"].([]any)
			for _, value := range entries {
				if !isClaudeHook(value, driga) {
					continue
				}
				entry := value.(map[string]any)
				command, _ := entry["command"].(string)
				if runtime.GOOS == "windows" && entry["args"] != nil {
					if !sameBinary(command, driga) {
						entry["command"] = driga
						d.Changed = true
					}
				} else if program, rest, ok := hookProgram(command); ok && !sameBinary(program, driga) {
					// Retain the event, matcher and any extra entry settings.
					prefix, err := hookCommand(driga, "claude", "", runtime.GOOS)
					if err != nil {
						return d, err
					}
					entry["command"] = strings.TrimSuffix(prefix, " hook claude ") + rest
					d.Changed = true
				}
			}
		}
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
		entry, err := hookEntry(driga, "claude", event, runtime.GOOS)
		if err != nil {
			return d, err
		}
		h[event] = append(groups, map[string]any{
			"matcher": "",
			"hooks":   []any{entry},
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

// ClaudeStale reports owned entries pointing at a missing or different binary.
func ClaudeStale(path, driga string) (bool, error) {
	_, settings, err := readClaude(path)
	if err != nil {
		return false, err
	}
	h, err := claudeHooks(settings)
	if err != nil {
		return false, err
	}
	for _, value := range h {
		groups, _ := value.([]any)
		for _, value := range groups {
			group, _ := value.(map[string]any)
			entries, _ := group["hooks"].([]any)
			for _, value := range entries {
				if !isClaudeHook(value, driga) {
					continue
				}
				entry := value.(map[string]any)
				command, _ := entry["command"].(string)
				program := command
				if runtime.GOOS != "windows" || entry["args"] == nil {
					program, _, _ = hookProgram(command)
				}
				if !binaryCurrent(program, driga) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// sameBinary follows symlinks and also recognizes hard links. Exact paths stay
// idempotent during preview even when an executable is not available yet.
func sameBinary(a, b string) bool {
	if a == b || runtime.GOOS == "windows" && pathx.Equal(a, b) {
		return true
	}
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}

func binaryCurrent(program, driga string) bool {
	info, err := os.Stat(program)
	return err == nil && info.Mode().IsRegular() && sameBinary(program, driga)
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
	if d.Before != "" {
		var original map[string]any
		dec := json.NewDecoder(strings.NewReader(d.Before))
		dec.UseNumber()
		if dec.Decode(&original) == nil && uniqueJSONMembers(d.Before) {
			edited := d.Before
			ok := true
			for _, key := range []string{"hooks", "env"} {
				if reflect.DeepEqual(original[key], settings[key]) {
					continue
				}
				var value []byte
				if v, exists := settings[key]; exists {
					value, err = json.MarshalIndent(v, "  ", "  ")
					if err != nil {
						return d, err
					}
				}
				edited, ok = editJSONMember(edited, key, value)
				if !ok {
					break
				}
			}
			if ok {
				d.After = edited
			} else {
				d.FullRewrite = true
			}
		} else {
			d.FullRewrite = true
		}
	}
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
	if args, exists := entry["args"]; runtime.GOOS == "windows" && exists {
		words, ok := args.([]any)
		return ok && len(words) == 3 && words[0] == "hook" && words[1] == "claude" && ours(command, driga)
	}
	program, rest, ok := hookProgram(command)
	return ok && ours(program, driga) && strings.HasPrefix(rest, " hook ")
}

func ours(program, driga string) bool {
	match := program == driga
	if runtime.GOOS == "windows" {
		match = pathx.Equal(program, driga)
	}
	return program != "" && ((driga != "" && match) || filepath.Base(program) == "driga" || runtime.GOOS == "windows" && strings.EqualFold(filepath.Base(program), "driga.exe"))
}

func currentBinary() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}

func shellQuote(s string) string { return textfmt.ShellQuote(s, false) }

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
	d.existed = true
	d.target, err = filepath.EvalSymlinks(path)
	return d, err
}

func writeDiff(d Diff, dryRun bool, now time.Time) error {
	if dryRun || !d.Changed {
		return nil
	}
	if err := d.Check(); err != nil {
		return err
	}
	if d.owned {
		return writeOwnedDiff(d)
	}
	path := d.Path
	if d.target != "" {
		path = d.target
	}
	_, err := os.Stat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if exists {
		// Keep each original, even when two writes fall in the same second.
		for stamp := now.Unix(); ; stamp++ {
			backup := fmt.Sprintf("%s.driga-bak-%d", d.Path, stamp)
			f, err := secfile.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
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
	} else if err := secfile.MkdirPrivate(filepath.Dir(path)); err != nil {
		return err
	}
	if err := secfile.WriteFileAtomic(path, []byte(d.After), d.Check); err != nil {
		return err
	}
	return pruneBackups(d.Path)
}

// Prune only recognized regular backups after a successful settings write.
// Keep the newest five per settings path, including the only backup.
func pruneBackups(path string) error {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	type backup struct {
		name  string
		stamp int64
	}
	var backups []backup
	prefix := filepath.Base(path) + ".driga-bak-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) || !entry.Type().IsRegular() {
			continue
		}
		stamp, err := strconv.ParseInt(strings.TrimPrefix(entry.Name(), prefix), 10, 64)
		if err == nil {
			backups = append(backups, backup{entry.Name(), stamp})
		}
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].stamp > backups[j].stamp })
	var errs []error
	for _, b := range backups[min(5, len(backups)):] {
		if err := os.Remove(filepath.Join(filepath.Dir(path), b.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Check refuses byte changes, creation/deletion, and symlink retargets since preview.
func (d Diff) Check() error {
	current, err := readDiff(d.Path)
	if err != nil {
		return err
	}
	if current.existed != d.existed || current.Before != d.Before || current.target != d.target {
		return fmt.Errorf("%s: settings changed since preview; rerun", d.Path)
	}
	return nil
}

// Apply writes exactly the previewed change, refusing intervening edits.
func (d Diff) Apply(now time.Time) error { return writeDiff(d, false, now) }

func otlpEndpoint(endpoints []string) string {
	if len(endpoints) > 0 {
		return strings.TrimRight(endpoints[0], "/")
	}
	return "http://127.0.0.1:7418"
}

// editJSONMember uses decoder offsets to leave unrelated values and whitespace intact.
func editJSONMember(src, key string, value []byte) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(src))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return src, false
	}
	open := int(dec.InputOffset())
	prevEnd := open
	for dec.More() {
		start := int(dec.InputOffset())
		tok, err = dec.Token()
		if err != nil {
			return src, false
		}
		name, ok := tok.(string)
		if !ok {
			return src, false
		}
		// Offset after the key; skip its colon and surrounding whitespace.
		vstart := int(dec.InputOffset())
		for vstart < len(src) && (src[vstart] == ':' || strings.ContainsRune(" \t\r\n", rune(src[vstart]))) {
			vstart++
		}
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return src, false
		}
		end := int(dec.InputOffset())
		if name == key {
			if value != nil {
				return src[:vstart] + string(value) + src[end:], true
			}
			if prevEnd == open {
				// Remove a first member and its following comma, keeping the next member's whitespace.
				next := end
				for next < len(src) && strings.ContainsRune(" \t\r\n", rune(src[next])) {
					next++
				}
				if next < len(src) && src[next] == ',' {
					end = next + 1
				}
				return src[:open] + src[end:], true
			}
			return src[:start] + src[end:], true // start includes the preceding comma
		}
		prevEnd = end
	}
	if value == nil {
		return src, true
	}
	comma := ""
	if prevEnd != open {
		comma = ","
	}
	return src[:open] + "\n  " + strconvQuote(key) + ": " + string(value) + comma + src[open:], true
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

// Duplicate keys make a byte edit ambiguous; a full rewrite resolves them.
func uniqueJSONMembers(src string) bool {
	dec := json.NewDecoder(strings.NewReader(src))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err = dec.Token()
		if err != nil {
			return false
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return false
		}
		seen[key] = true
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return false
		}
	}
	return true
}
