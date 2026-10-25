package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/labstack/driga/internal/secfile"
)

// Set writes bool and string settings into the TOML file at path, creating it
// if missing. It edits the lines of those keys in place, or adds them to their
// section, so the user's comments and other settings stay as written. The file
// is replaced only when the result loads. A symlinked file is written at its
// target, so the link stays.
func Set(path string, values map[string]any) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	byKey := map[string]Setting{}
	for _, s := range settings {
		byKey[s.Key] = s
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(b) == 0 {
		lines = nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s, ok := byKey[key]
		if !ok {
			return fmt.Errorf("unknown setting %s", key)
		}
		var text string
		switch v := values[key].(type) {
		case bool:
			text = strconv.FormatBool(v)
		case string:
			text = strconv.Quote(v)
		default:
			return fmt.Errorf("%s: only bool and string settings can be set", key)
		}
		if s.Type != "bool" && s.Type != "string" {
			return fmt.Errorf("%s: only bool and string settings can be set", key)
		}
		lines = setLine(lines, key, text)
	}
	out := []byte(strings.Join(lines, "\n") + "\n")
	c := Default()
	if err := applyFile(path, out, &c, map[string]string{}, byKey); err != nil {
		return err
	}
	if err := secfile.MkdirPrivate(filepath.Dir(path)); err != nil {
		return err
	}
	return secfile.WriteFileAtomic(path, out, nil)
}

// setLine replaces the line assigning key, or adds one: under the key's
// section header when the file has one, else in a new section at the end.
func setLine(lines []string, key, value string) []string {
	section, leaf := "", key
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		section, leaf = key[:i], key[i+1:]
	}
	current, header := "", -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			current = strings.TrimSpace(strings.Trim(strings.SplitN(trimmed, "#", 2)[0], "[] \t"))
			if current == section {
				header = i
			}
			continue
		}
		name, old, ok := strings.Cut(trimmed, "=")
		if !ok || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name = strings.TrimSpace(name)
		if current == section && name == leaf || current == "" && name == key {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + name + " = " + value + inlineComment(old)
			return lines
		}
	}
	assignment := leaf + " = " + value
	switch {
	case header >= 0:
		return append(lines[:header+1], append([]string{assignment}, lines[header+1:]...)...)
	case section == "":
		return append([]string{assignment}, lines...)
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	return append(lines, "["+section+"]", assignment)
}

// inlineComment is the comment after a bool or string value, with the space
// before it, or "" when there is none.
func inlineComment(value string) string {
	value = strings.TrimLeft(value, " \t")
	end := 0
	switch {
	case strings.HasPrefix(value, `"`):
		end = 1
		for end < len(value) && value[end] != '"' {
			if value[end] == '\\' {
				end++
			}
			end++
		}
		end++
	case strings.HasPrefix(value, "'"):
		end = strings.IndexByte(value[1:], '\'') + 2
		if end == 1 {
			end = len(value)
		}
	default:
		end = strings.IndexAny(value, " \t#")
		if end < 0 {
			end = len(value)
		}
	}
	rest := value[min(end, len(value)):]
	if !strings.HasPrefix(strings.TrimLeft(rest, " \t"), "#") {
		return ""
	}
	return strings.TrimRight(rest, " \t")
}
