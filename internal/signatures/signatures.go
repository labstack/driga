// Package signatures classifies processes using ordered TOML signature files.
package signatures

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

// Role describes a process's place in an agent session or desktop application.
type Role string

const (
	RoleInternal    Role = "internal"    // Driga's own Claude invocation.
	RoleInteractive Role = "interactive" // An interactive agent session.
	RoleHeadless    Role = "headless"    // A non-interactive agent run.
	RoleHost        Role = "host"        // A long-lived host of multiple threads.
	RoleHelper      Role = "helper"      // A broker or MCP server.
	RoleApp         Role = "app"         // A desktop app's main executable.
)

// Signature identifies a process when any of its Match regexes matches.
type Signature struct {
	Name      string   `toml:"name"`
	Agent     string   `toml:"agent"`
	Role      Role     `toml:"role"`
	Match     []string `toml:"match"`
	AppBundle bool     `toml:"app_bundle"`
}

type compiledSignature struct {
	signature Signature
	patterns  []*regexp.Regexp
}

// Set holds signatures in precedence order, with their compiled regexes.
type Set struct {
	entries []compiledSignature
}

//go:embed signatures.toml
var embedded []byte

// Load reads extra TOML files in order, followed by the embedded signatures.
// Every regex is validated before a set is returned.
func Load(extra []string) (*Set, error) {
	set := &Set{}
	for _, path := range extra {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("signatures %s: %w", path, err)
		}
		if err := set.appendFile(path, data); err != nil {
			return nil, err
		}
	}
	if err := set.appendFile("embedded signatures.toml", embedded); err != nil {
		return nil, err
	}
	return set, nil
}

func (s *Set) appendFile(path string, data []byte) error {
	var file struct {
		Signatures []Signature `toml:"signature"`
	}
	if err := toml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("signatures %s: %w", path, err)
	}
	for _, signature := range file.Signatures {
		entry := compiledSignature{signature: signature}
		for _, pattern := range signature.Match {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("signatures %s: signature %q: %w", path, signature.Name, err)
			}
			entry.patterns = append(entry.patterns, re)
		}
		s.entries = append(s.entries, entry)
	}
	return nil
}

// Match returns the first matching signature, using exe only if cmdline is empty.
func (s *Set) Match(cmdline, exe string) (Signature, bool) {
	if cmdline == "" {
		cmdline = exe
	}
	for _, entry := range s.entries {
		for _, pattern := range entry.patterns {
			if pattern.MatchString(cmdline) {
				return entry.signature, true
			}
		}
	}
	return Signature{}, false
}

var bundlePathPattern = regexp.MustCompile(`^["']?((?:/(?:[^/[:space:]"']+/)*|~/)Applications/[^/]+\.app)/Contents/`)

// BundlePath returns the enclosing application bundle of an executable or
// command line, including helpers elsewhere inside the bundle's Contents.
// A leading ~/ is preserved; no filesystem access is needed.
func BundlePath(cmdlineOrExe string) (string, bool) {
	match := bundlePathPattern.FindStringSubmatch(cmdlineOrExe)
	if match == nil {
		return "", false
	}
	return match[1], true
}
