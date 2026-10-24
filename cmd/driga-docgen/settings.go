package main

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/labstack/driga/internal/config"
)

// settingGroups names each TOML table. A setting whose table is missing here
// fails generation rather than landing under a heading nobody chose.
var settingGroups = []struct{ prefix, title, intro string }{
	{"", "General", "Top-level keys of `config.toml`."},
	{"ui", "Terminal UI", "The `[ui]` table."},
	{"adapters", "Agent adapters", "One `[adapters.<name>]` table per agent."},
	{"orphans", "Orphans", "The `[orphans]` table."},
	{"machine", "Machine health", "The `[machine]` table."},
	{"summaries", "Summaries", "The `[summaries]` table. Summaries run your installed Claude CLI on demand."},
	{"cleanup", "Cleanup", "The `[cleanup]` table."},
	{"fleet", "Fleet", "The `[fleet]` table for paired machines."},
	{"inbox", "Inbox and alerts", "The `[inbox]` table."},
	{"intake", "OTLP intake", "The `[intake]` table for the optional loopback OTLP receiver."},
}

func settingGroup(key string) string {
	group, _, ok := strings.Cut(key, ".")
	if !ok {
		return ""
	}
	return group
}

func renderSettings() string {
	settings := config.Settings()
	var b strings.Builder
	frontmatter(&b,
		"Settings reference",
		"Every driga configuration key with its type, default, allowed values and DRIGA_* environment variable.",
		"Every configuration key, its type, default, allowed values and environment variable, generated from the settings table that also writes .env.example.",
		"You want to change a default in config.toml or through the environment.",
		"A DRIGA_* variable is rejected as unknown and you want the valid names.",
	)
	b.WriteString("Driga reads `config.toml` and the process environment. Precedence is explicit\n")
	b.WriteString("flags, then environment, then the TOML file, then the defaults below. Empty\n")
	b.WriteString("environment values count as unset, and dotenv files are never loaded. An\n")
	b.WriteString("unknown `DRIGA_*` name is an error. Restart the collector after changing its\n")
	b.WriteString("configuration; the CLI and hooks reload on every invocation.\n\n")
	b.WriteString("Find and edit the file with `driga config path`, `driga config show` and\n")
	b.WriteString("`driga config edit`. List settings are file-only and have no environment\n")
	b.WriteString("variable.\n\n")

	b.WriteString("## Summary\n\n")
	b.WriteString("| Key | Type | Default | Environment |\n|---|---|---|---|\n")
	for _, s := range settings {
		env := "file only"
		if name := s.EnvName(); name != "" {
			env = code(name)
		}
		fmt.Fprintf(&b, "| [%s](#%s) | %s | %s | %s |\n", code(s.Key), settingAnchor(s.Key), s.Type, defaultValue(s.Default), env)
	}
	b.WriteString("\n")

	seen := 0
	for _, group := range settingGroups {
		var members []config.Setting
		for _, s := range settings {
			if settingGroup(s.Key) == group.prefix {
				members = append(members, s)
			}
		}
		if len(members) == 0 {
			continue
		}
		seen += len(members)
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", group.title, text(group.intro))
		for _, s := range members {
			fmt.Fprintf(&b, "### %s\n\n", s.Key)
			fmt.Fprintf(&b, "%s\n\n", text(sentence(s.Purpose)))
			b.WriteString("| Type | Default | Allowed | Environment |\n|---|---|---|---|\n")
			env := "file only"
			if name := s.EnvName(); name != "" {
				env = code(name)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n\n", s.Type, defaultValue(s.Default), cell(s.Range), env)
		}
	}
	if seen != len(settings) {
		panic(fmt.Sprintf("settings reference covers %d of %d settings; add the missing table to settingGroups", seen, len(settings)))
	}

	var paths, internal []config.RuntimeSetting
	for _, s := range config.RuntimeSettings() {
		if s.Type == "path" && s.Default != "unset" {
			paths = append(paths, s)
		} else {
			internal = append(internal, s)
		}
	}
	b.WriteString("## Path overrides\n\n")
	b.WriteString("Relative values resolve against the working directory at startup.\n\n")
	b.WriteString("| Variable | Default | Purpose |\n|---|---|---|\n")
	for _, s := range paths {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", code(s.Name), cell(s.Default), cell(s.Purpose))
	}
	b.WriteString("\n## Internal names\n\n")
	b.WriteString("Driga sets these for its own child processes and tests. They are accepted so\n")
	b.WriteString("those processes start cleanly; they are not configuration.\n\n")
	b.WriteString("| Variable | Kind | Purpose |\n|---|---|---|\n")
	for _, s := range internal {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", code(s.Name), s.Type, cell(s.Purpose))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func defaultValue(v any) string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice {
		if rv.Len() == 0 {
			return "empty"
		}
		items := make([]string, rv.Len())
		for i := range rv.Len() {
			items[i] = code(fmt.Sprint(rv.Index(i).Interface()))
		}
		return strings.Join(items, ", ")
	}
	s := fmt.Sprint(v)
	if s == "" {
		return "empty"
	}
	return code(s)
}

// settingAnchor is the slug Starlight gives a heading that is the key itself.
func settingAnchor(key string) string {
	return strings.ReplaceAll(strings.ToLower(key), ".", "")
}
