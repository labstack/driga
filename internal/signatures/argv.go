package signatures

import (
	"path"
	"strings"
)

// Keep executable paths with spaces usable by the legacy regex signatures.
// This view is only for matching; snapshots and the caller's argv stay untouched.
var tokenWhitespace = strings.NewReplacer(" ", "%20", "\t", "%09", "\n", "%0A", "\r", "%0D", "\v", "%0B", "\f", "%0C")

func signatureInput(cmdline, exe string, argv []string) (string, string) {
	if len(argv) == 0 {
		return windowsSignatureInput(cmdline, exe)
	}
	args := append([]string(nil), argv...)
	if windowsPathShape(exe) || windowsPathShape(args[0]) {
		exe = windowsSignaturePath(exe, true)
		args[0] = windowsSignaturePath(args[0], true)
		if len(args) > 1 {
			args[1] = windowsSignaturePath(args[1], false)
		}
		// Retain the existing Windows regex view. Unix argument text keeps
		// its original whitespace for user-supplied regex signatures.
		for i := range args {
			args[i] = tokenWhitespace.Replace(args[i])
		}
	}
	args[0] = tokenWhitespace.Replace(args[0])
	return strings.Join(args, " "), exe
}

func commandModeInput(argv []string, exe string) (string, []string) {
	if len(argv) == 0 {
		return "", nil
	}
	token := argv[0]
	if windowsPathShape(token) || windowsPathShape(exe) {
		token, exe = windowsSignaturePath(token, true), windowsSignaturePath(exe, true)
	}
	name := path.Base(token)
	if agent, version := versionedName(token); version != "" {
		name = agent
	} else if bareVersion.MatchString(token) {
		if agent, version := versionedName(exe); version != "" {
			name = agent
		}
	}
	args := argv[1:]
	if name == "codex" {
		for len(args) > 0 {
			switch {
			case args[0] == "-c" || args[0] == "--config":
				if len(args) < 2 {
					return name, nil
				}
				args = args[2:]
			case strings.HasPrefix(args[0], "-c=") || strings.HasPrefix(args[0], "--config="):
				args = args[1:]
			default:
				return name, args
			}
		}
	}
	return name, args
}

func matchesCommandMode(sig Signature, exe string, args []string) bool {
	if exe == "" || exe != sig.Agent {
		return false
	}
	for _, mode := range sig.CommandModes {
		if len(mode) == 0 || len(args) < len(mode) {
			continue
		}
		matched := true
		for i, token := range mode {
			if args[i] != token && !(strings.HasPrefix(token, "--") && strings.HasPrefix(args[i], token+"=")) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
