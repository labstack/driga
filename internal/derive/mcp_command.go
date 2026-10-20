package derive

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

// Identity is independent of the runtime installation, but retains server
// arguments and package scopes. Display names are never used as group keys.
type mcpIdentity struct {
	name, key, entry, runtime, pkg, target string
	args                                   []string
	shim                                   bool
}

// npm ships native builds as per-platform packages (esbuild-darwin-arm64);
// the platform suffix is not part of the server's identity.
var npmPlatformSuffix = regexp.MustCompile(`-(?:darwin|linux|win32|freebsd|android|sunos|aix)-(?:arm64|x64|ia32|arm|ppc64|s390x|riscv64)(?:-(?:gnu|musl|msvc|gnueabihf))?$`)

var executableVersion = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)+(?:[-+][0-9A-Za-z.-]+)?$`)

func describeMCP(p model.Proc, sigs *signatures.Set) mcpIdentity {
	words := commandWords(p.Cmdline)
	if len(words) == 0 {
		if p.Cmdline != "" || p.Exe == "" {
			return mcpIdentity{}
		}
		words = []string{p.Exe}
	}
	d := mcpIdentity{entry: words[0], args: words[1:]}
	exe := words[0]
	if p.Exe != "" {
		exe = p.Exe
	}
	if path.Base(words[0]) == "node" || path.Base(exe) == "node" {
		d.runtime = exe
		i := 1
		for i < len(words) && strings.HasPrefix(words[i], "-") {
			option := words[i]
			i++
			switch option {
			case "--require", "-r", "--import", "--loader", "--experimental-loader", "--disable-warning":
				i++
			}
		}
		if i < len(words) {
			d.entry, d.args = words[i], words[i+1:]
		}
	}
	base := path.Base(d.entry)
	// npm can rewrite argv to "node exec X". Also accept explicit npx/npm
	// entrypoints, including npm's JavaScript bin shims.
	switch base {
	case "exec":
		if d.runtime != "" {
			d.target, d.args = npmTarget(d.args)
		}
	case "npx", "npx-cli.js":
		d.target, d.args = npmTarget(d.args)
	case "npm", "npm-cli.js":
		if len(d.args) > 0 && d.args[0] == "exec" {
			d.target, d.args = npmTarget(d.args[1:])
		}
	}
	if d.target != "" {
		d.name = packageWithoutVersion(d.target)
		d.key = "npm:" + d.name
		return d
	}
	if bundle := AppBundle(d.entry); bundle != "" {
		d.name = strings.TrimSuffix(path.Base(bundle), ".app") + " · "
		// Bundled runtimes carry their tool name in <tool>_node/bin/<bin>.
		tool := path.Base(path.Dir(path.Dir(d.entry)))
		if path.Base(path.Dir(d.entry)) == "bin" && strings.HasSuffix(tool, "_node") {
			d.name += strings.TrimSuffix(tool, "_node") + " "
		}
		d.name += base
		d.key = "app:" + d.entry
	} else if pkg := npmPackage(d.entry); pkg != "" {
		d.pkg = pkg
		d.name = npmPlatformSuffix.ReplaceAllString(path.Base(pkg), "")
		if d.name == "mcp" && strings.HasPrefix(pkg, "@") {
			d.name = pkg
		}
		d.key = "npm:" + npmPlatformSuffix.ReplaceAllString(pkg, "")
	} else if path.Base(path.Dir(d.entry)) == ".bin" || (d.runtime != "" && path.Base(path.Dir(d.entry)) == "bin" && path.Ext(d.entry) == "") {
		d.name, d.key = base, "npm:"+base
		d.shim = true
	} else {
		d.name = base
		if executableVersion.MatchString(path.Base(exe)) {
			d.name = sigs.ProcessName(p.Cmdline, exe)
		} else if before, _, ok := strings.Cut(d.entry, "/target/"); ok {
			// Cargo build paths identify the project even for binaries called server.
			d.name = path.Base(path.Dir(before)) + "/" + path.Base(before)
		}
		if name, ok := sigs.MCPName(p.Cmdline, p.Exe); ok {
			d.name = name
		}
		d.key = "command:" + normalizedMCPCommand(p)
	}
	// Preserve readable subcommands, leaving options and paths to details.
	if !strings.HasPrefix(d.key, "npm:") {
		for _, arg := range d.args {
			if strings.HasPrefix(arg, "-") || strings.Contains(arg, "/") || strings.Contains(arg, ".") {
				break
			}
			if !strings.HasSuffix(d.name, " "+arg) {
				d.name += " " + arg
			}
		}
	}
	return d
}

func npmTarget(args []string) (string, []string) {
	for len(args) > 0 {
		switch args[0] {
		case "--", "-y", "--yes", "--no":
			args = args[1:]
		default:
			if strings.HasPrefix(args[0], "-") {
				return "", args
			}
			return args[0], args[1:]
		}
	}
	return "", nil
}

func packageWithoutVersion(pkg string) string {
	if i := strings.LastIndex(pkg, "@"); i > 0 {
		return pkg[:i]
	}
	return pkg
}

func npmPackage(entry string) string {
	_, rest, ok := strings.Cut(entry, "/node_modules/")
	if !ok {
		return ""
	}
	for strings.Contains(rest, "/node_modules/") {
		_, rest, _ = strings.Cut(rest, "/node_modules/")
	}
	parts := strings.Split(rest, "/")
	if parts[0] == ".bin" {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 3 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func (d mcpIdentity) groupKey() string {
	if !strings.HasPrefix(d.key, "npm:") && !strings.HasPrefix(d.key, "app:") {
		return d.key
	}
	// Reuse volatile port/temp normalization while retaining all server options.
	return d.key + "\x00" + normalizedMCPCommand(model.Proc{Cmdline: joinCommand(append([]string{"server"}, d.args...))})
}

func joinCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = strconv.Quote(arg)
	}
	return strings.Join(quoted, " ")
}

func launchesMCP(parent, child mcpIdentity) bool {
	if parent.target != "" {
		target := packageWithoutVersion(parent.target)
		name := strings.TrimSuffix(path.Base(child.entry), path.Ext(child.entry))
		match := target == child.pkg || target == name
		if strings.HasPrefix(target, "@") && child.pkg == "" {
			match = match || path.Base(target) == name || strings.ReplaceAll(strings.TrimPrefix(target, "@"), "/", "-") == name
		}
		return match && slices.Equal(parent.args, child.args)
	}
	// Identical commands can be server workers. Only a bin shim handing off to
	// a different package entrypoint is evidence of a launcher.
	return parent.shim && child.pkg != "" && parent.entry != child.entry && parent.key == child.key && slices.Equal(parent.args, child.args)
}
