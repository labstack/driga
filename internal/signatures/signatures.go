// Package signatures classifies processes using ordered TOML signature files.
package signatures

import (
	_ "embed"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// Role describes a process's place in an agent session or desktop application.
type Role string

const (
	RoleInternal    Role = "internal"    // Driga's own Claude invocation.
	RoleService     Role = "service"     // A managed background service; never an orphan
	RoleInteractive Role = "interactive" // An interactive agent session.
	RoleHeadless    Role = "headless"    // A non-interactive agent run.
	RoleHost        Role = "host"        // A long-lived host of multiple threads.
	RoleHelper      Role = "helper"      // A broker or MCP server.
	RoleApp         Role = "app"         // A desktop app's main executable.
)

// Signature identifies a process by command-mode argv or Match regexes.
type Signature struct {
	Name      string   `toml:"name"`
	Agent     string   `toml:"agent"`
	Role      Role     `toml:"role"`
	Match     []string `toml:"match"`
	AppBundle bool     `toml:"app_bundle"`
	HostName  string   `toml:"host_name"`
	// ProtectTree excludes this helper and its descendants from orphan cleanup.
	ProtectTree bool `toml:"protect_tree"`
	// IdleWithoutClients applies when this helper roots an orphan tree: only a
	// connection to another of the user's processes, or the network, shows the
	// tree is in use. Listeners and system-service sockets do not. Set it only
	// when clients start a replacement once the helper is gone.
	IdleWithoutClients bool `toml:"idle_without_clients"`
	// CommandModes matches argv prefixes after the executable (Agent), skipping
	// leading Codex config overrides. These entries never inspect joined text.
	CommandModes [][]string `toml:"command_modes"`
	// ExecutableMatch optionally restricts a command mode to an installation
	// path in argv[0] or the resolved executable, never another argument.
	ExecutableMatch string `toml:"executable_match"`
}

type compiledSignature struct {
	signature  Signature
	patterns   []*regexp.Regexp
	executable *regexp.Regexp
}

// Set holds signatures in precedence order, with their compiled regexes.
type Set struct {
	entries []compiledSignature
	mcp     []compiledSignature
}

// Builtin shares immutable embedded signatures across read-only derived views.
var Builtin = sync.OnceValue(func() *Set {
	s, err := Load(nil)
	if err != nil {
		panic(err)
	} // Invalid embedded signatures are a build defect.
	return s
})

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
		if err := set.appendFile(path, data, ""); err != nil {
			return nil, err
		}
	}
	if err := set.appendFile("embedded signatures.toml", embedded, ""); err != nil {
		return nil, err
	}
	return set, nil
}

// LoadAgent compiles only an agent's embedded signatures for a short-lived hook.
func LoadAgent(agent string) (*Set, error) {
	set := &Set{}
	err := set.appendFile("embedded signatures.toml", embedded, agent)
	return set, err
}

func (s *Set) appendFile(path string, data []byte, agent string) error {
	var file struct {
		Signatures []Signature `toml:"signature"`
		MCPServers []Signature `toml:"mcp_server"`
	}
	if err := toml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("signatures %s: %w", path, err)
	}
	for _, signature := range file.Signatures {
		if agent != "" && signature.Agent != agent {
			continue
		}
		entry := compiledSignature{signature: signature}
		if signature.ExecutableMatch != "" {
			re, err := regexp.Compile(signature.ExecutableMatch)
			if err != nil {
				return fmt.Errorf("signatures %s: signature %q executable: %w", path, signature.Name, err)
			}
			entry.executable = re
		}
		for _, pattern := range signature.Match {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("signatures %s: signature %q: %w", path, signature.Name, err)
			}
			entry.patterns = append(entry.patterns, re)
		}
		s.entries = append(s.entries, entry)
	}
	// Hooks classify their parent agent; they never label MCP servers.
	if agent != "" {
		return nil
	}
	for _, signature := range file.MCPServers {
		entry := compiledSignature{signature: signature}
		for _, pattern := range signature.Match {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("signatures %s: MCP server %q: %w", path, signature.Name, err)
			}
			entry.patterns = append(entry.patterns, re)
		}
		s.mcp = append(s.mcp, entry)
	}
	return nil
}

// Match returns the first matching signature. argv must come from the platform,
// never from splitting Cmdline: Unix snapshots join it without shell escaping.
// Without argv, regex signatures still apply, but command modes cannot match.
func (s *Set) Match(cmdline, exe string, argv []string) (Signature, bool) {
	modeExe, modeArgs := commandModeInput(argv, exe)
	modePath := ""
	if len(argv) > 0 {
		modePath = argv[0]
		if windowsPathShape(modePath) || windowsPathShape(exe) {
			modePath = windowsSignaturePath(modePath, true)
		}
	}
	cmdline, exe = signatureInput(cmdline, exe, argv)
	if cmdline == "" {
		cmdline = exe
	}
	for _, entry := range s.entries {
		if len(entry.signature.CommandModes) != 0 {
			if entry.executable != nil {
				if !entry.executable.MatchString(modePath) && !entry.executable.MatchString(exe) {
					continue
				}
			}
			if matchesCommandMode(entry.signature, modeExe, modeArgs) {
				return entry.signature, true
			}
			continue
		}
		for _, pattern := range entry.patterns {
			if pattern.MatchString(cmdline) {
				return entry.signature, true
			}
		}
	}
	// Some agents execute a bare version filename. Canonicalize only the
	// executable token; arguments retain the signature's usual precedence.
	token, rest, hasArgs := strings.Cut(cmdline, " ")
	name, version := versionedName(token)
	if version == "" && bareVersion.MatchString(token) {
		name, version = versionedName(exe)
	}
	if version == "" {
		return Signature{}, false
	}
	cmdline = name
	if hasArgs {
		cmdline += " " + rest
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

var bareVersion = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func versionedName(exe string) (string, string) {
	version := path.Base(exe)
	if !bareVersion.MatchString(version) {
		return "", ""
	}
	dir := path.Dir(exe)
	for {
		name := path.Base(dir)
		switch name {
		case "versions", "releases", "bin":
			dir = path.Dir(dir)
		case ".", "/":
			return "", ""
		default:
			return name, version
		}
	}
}

// ProcessName labels an agent by signature and preserves a versioned executable's
// version. Unclassified bare versions use the nearest meaningful directory.
func (s *Set) ProcessName(cmdline, exe string, argv []string) string {
	sig, ok := s.Match(cmdline, exe, argv)
	cmdline, exe = signatureInput(cmdline, exe, argv)
	name, version := versionedName(exe)
	if exe == "" {
		token, _, _ := strings.Cut(cmdline, " ")
		name, version = versionedName(token)
	}
	if ok && sig.Agent != "" {
		name = sig.Agent
		if sig.HostName != "" {
			name = sig.HostName
		}
	}
	if name == "" {
		name = path.Base(exe)
	}
	if exe == "" && name == "." {
		name = "unknown"
	}
	if version != "" {
		name += " " + version
	}
	return name
}

// MCPName labels known report-only MCP commands. These patterns deliberately do
// not add helper/orphan signatures or authorize any process action.
func (s *Set) MCPName(cmdline, exe string) (string, bool) {
	cmdline, exe = windowsSignatureInput(cmdline, exe)
	if cmdline == "" {
		cmdline = exe
	}
	for _, entry := range s.mcp {
		for _, pattern := range entry.patterns {
			if pattern.MatchString(cmdline) {
				return entry.signature.Name, true
			}
		}
	}
	return "", false
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
