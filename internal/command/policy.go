package command

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/labstack/driga/internal/config"
)

// canonical resolves existing ancestors too, so a missing leaf cannot hide a
// symlink escape. Preparation never creates a requested path.
func canonical(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		q, e := filepath.EvalSymlinks(p)
		if e == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				q = filepath.Join(q, tail[i])
			}
			return q, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", e
		}
		tail = append(tail, filepath.Base(p))
		p = parent
	}
}
func within(p, root string) bool {
	if root == "" {
		return false
	}
	r, e := filepath.Rel(root, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func normalizeScope(s Scope) (Scope, error) {
	var err error
	if s.Project != "" {
		s.Project, err = canonical(s.Project)
		if err != nil {
			return s, err
		}
	}
	if s.Scratch != "" {
		s.Scratch, err = canonical(s.Scratch)
		if err != nil {
			return s, err
		}
	}
	s.Denied = append([]string(nil), s.Denied...)
	s.Denied = append(s.Denied, config.Path(), config.DataDir(), config.DefaultDataDir(), config.SpoolDir())
	if h, e := os.UserHomeDir(); e == nil {
		for _, p := range []string{".claude", ".codex", ".ssh", ".aws", ".azure", ".kube", ".docker", ".config/gh", ".config/azure", ".config/gcloud", ".netrc", ".npmrc", ".git-credentials", ".config/driga", ".local/share/driga", "Library/Keychains", "Library/Application Support/driga"} {
			s.Denied = append(s.Denied, filepath.Join(h, p))
		}
		// Claude also uses sibling files and directories (e.g. .claude.json).
		matches, _ := filepath.Glob(filepath.Join(h, ".claude*"))
		s.Denied = append(s.Denied, matches...)
	}
	if filepath.Separator == '/' {
		s.Denied = append(s.Denied, "/Library/Keychains")
	}
	for i, d := range s.Denied {
		s.Denied[i], err = canonical(d)
		if err != nil {
			return s, err
		}
	}
	slices.Sort(s.Denied)
	s.Denied = slices.Compact(s.Denied)
	return s, nil
}
func denied(p string, s Scope) bool {
	for _, d := range s.Denied {
		if within(p, d) {
			return true
		}
	}
	return false
}

// Resolve bare commands against system directories, never a project PATH entry.
// Explicit paths keep their meaning but acquire Read authority only when they
// resolve to the same system executable, with the same basename.
func resolve(name, cwd string) Executable {
	var path string
	if strings.ContainsAny(name, "/\\") {
		path = name
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
	} else {
		for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/usr/local/bin", "/opt/homebrew/bin"} {
			p := filepath.Join(dir, name)
			if st, e := os.Stat(p); e == nil && st.Mode().IsRegular() && st.Mode()&0111 != 0 {
				path = p
				break
			}
		}
	}
	if path == "" {
		return Executable{Path: name}
	}
	path, e := canonical(path)
	if e != nil {
		return Executable{Path: path}
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return Executable{Path: path}
	}
	return Executable{Path: path, SHA256: sha256.Sum256(b)}
}
func trusted(e Executable, name string) bool {
	if filepath.Base(name) != name || e.SHA256 == ([32]byte{}) {
		return false
	}
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		p, err := canonical(filepath.Join(dir, name))
		if err == nil && p == e.Path {
			return true
		}
	}
	return false
}

func Classify(p Parsed, scope Scope) (Policy, error) {
	return classify(p, scope, 0)
}

func classify(p Parsed, scope Scope, depth int) (Policy, error) {
	if depth >= 16 {
		return Policy{Class: Destructive, Reason: "Nested execution requires approval"}, nil
	}
	s, err := normalizeScope(scope)
	if err != nil {
		return Policy{}, err
	}
	pol := Policy{Class: Other, Reason: "Sandboxed command requires authorization"}
	allRead := !p.Complex && len(p.Commands) > 0
	readScoped := scopedReadPath(p.Request.Cwd, "", s)
	var process, destructive bool
	for _, args := range p.Commands {
		if len(args) == 0 || args[0] == "" {
			allRead = false
			continue
		}
		name := filepath.Base(args[0])
		lower := strings.TrimSuffix(strings.ToLower(name), ".exe")
		candidate := args[0]
		if strings.ContainsAny(candidate, "/\\") {
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(p.Request.Cwd, candidate)
			}
			q, e := canonical(candidate)
			if e != nil {
				return Policy{}, e
			}
			if denied(q, s) {
				return Policy{Class: Never, Reason: "Protected executable is refused"}, nil
			}
		}
		exe := resolve(args[0], p.Request.Cwd)
		exe.Argv = append([]string(nil), args...)
		if lower == "git" {
			// Ignore repository/system/user configuration and helper attributes. A Git
			// read must never execute a pager, external diff, textconv or fsmonitor.
			exe.Argv = append([]string{args[0], "--no-pager", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "diff.external=", "-c", "core.attributesFile=/dev/null"}, args[1:]...)
			if len(args) > 1 && slices.Contains([]string{"diff", "show", "log"}, args[1]) {
				prefix := len(exe.Argv) - len(args) + 2
				exe.Argv = append(append(exe.Argv[:prefix:prefix], "--no-ext-diff", "--no-textconv"), exe.Argv[prefix:]...)
			}
		}
		pol.Commands = append(pol.Commands, exe)
		if slices.Contains([]string{"sudo", "sudoedit", "su", "doas", "pkexec", "runuser", "chroot", "gosu"}, lower) {
			return Policy{Class: Never, Reason: "Privilege escalation is refused"}, nil
		}
		if lower == "systemctl" || lower == "launchctl" {
			return Policy{Class: Never, Reason: "Service control is refused"}, nil
		}
		for _, a := range args[1:] {
			if strings.HasPrefix(a, "-") {
				if _, v, ok := strings.Cut(a, "="); ok {
					a = v
				} else {
					continue
				}
			}
			if a == "" {
				continue
			}
			path := a
			if !filepath.IsAbs(path) {
				path = filepath.Join(p.Request.Cwd, path)
			}
			q, e := canonical(path)
			if e != nil {
				allRead = false
				continue
			}
			if denied(q, s) {
				return Policy{Class: Never, Reason: "Protected credential or Driga path is refused"}, nil
			}
		}
		if slices.Contains([]string{"kill", "pkill", "killall", "taskkill", "stop-process"}, lower) {
			process = true
		}
		if lower == "rm" {
			options := true
			for _, a := range args[1:] {
				if options && a == "--" {
					options = false
					continue
				}
				if options && strings.HasPrefix(a, "-") {
					continue
				}
				q := a
				if !filepath.IsAbs(q) {
					q = filepath.Join(p.Request.Cwd, q)
				}
				q, e := canonical(q)
				if e != nil || !within(q, s.Scratch) {
					destructive = true
				}
			}
		}
		if slices.Contains([]string{"touch", "mkdir", "rmdir", "mv", "cp", "install", "tee", "truncate", "ln", "chmod", "chown"}, lower) {
			options := true
			for _, a := range args[1:] {
				if options && a == "--" {
					options = false
					continue
				}
				if options && strings.HasPrefix(a, "-") {
					switch {
					case strings.HasPrefix(a, "--target-directory="):
						a = strings.TrimPrefix(a, "--target-directory=")
					case slices.Contains([]string{"cp", "mv", "install", "ln"}, lower) && strings.HasPrefix(a, "-t") && len(a) > 2:
						a = a[2:]
					default:
						continue
					}
				}
				if a == "" {
					continue
				}
				q := a
				if !filepath.IsAbs(q) {
					q = filepath.Join(p.Request.Cwd, q)
				}
				q, e := canonical(q)
				if e != nil || !within(q, s.Project) && !within(q, s.Scratch) {
					destructive = true
				}
			}
		}
		if slices.Contains([]string{"eval", "source", ".", "builtin", "env", "command", "exec", "xargs", "nice", "timeout", "nohup", "busybox", "toybox", "setsid", "stdbuf", "time", "ionice", "chrt", "setpriv", "flock", "unbuffer", "script", "watch", "parallel", "sh", "bash", "zsh", "dash", "ksh", "fish", "csh", "tcsh", "powershell", "pwsh", "cmd"}, lower) || slices.Contains([]string{"find", "rg"}, lower) && !readArgs(lower, args[1:]) {
			// Wrappers may change cwd, environment, PATH, substitute filenames or
			// read a command from stdin. Their execution is never automatic. Inspect
			// literal nested commands as well so wrappers cannot weaken Never.
			// Destructive is the approval floor even if parsing fails or the nested
			// command is Read, Other or Process; only Never is more restrictive.
			destructive = true
			for _, nested := range wrappedCommands(lower, args[1:]) {
				np := Parsed{Request: Request{Cwd: p.Request.Cwd}, Commands: [][]string{nested}}
				npol, err := classify(np, s, depth+1)
				if err != nil {
					return Policy{}, err
				}
				if npol.Class == Never {
					return npol, nil
				}
			}
		}
		if lower == "git" {
			i, approval := gitSubcommand(args)
			if approval || !trusted(exe, args[0]) || !readArgs(lower, args[1:]) {
				// Unknown subcommands may be configured shell aliases, and
				// unsupported read options can re-enable external helpers. Only
				// our validated, hardened read grammar is automatic for Git.
				destructive = true
			}
			if i >= 0 && (slices.Contains([]string{"push", "clean"}, args[i]) || args[i] == "reset" && slices.Contains(args[i+1:], "--hard") || args[i] == "branch" && slices.Contains(args[i+1:], "-D") || args[i] == "checkout" && slices.Contains(args[i+1:], "--")) {
				destructive = true
			}
		}
		if lower == "docker" && len(args) > 1 && (slices.Contains([]string{"rm", "rmi"}, args[1]) || args[1] == "volume" && len(args) > 2 && args[2] == "rm") {
			destructive = true
		}
		if !trusted(exe, args[0]) || !readArgs(lower, args[1:]) {
			allRead = false
		} else {
			for _, path := range readPaths(lower, args[1:], p.Request.Cwd) {
				if !scopedReadPath(path, p.Request.Cwd, s) {
					readScoped = false
				}
			}
		}
	}
	// Compound/expanded scripts may conceal writes or process selectors. They
	// require a card and a sandbox; a static embedded kill never grants host access.
	if p.Complex {
		destructive = true
		process = false
	}
	pol.RequiresCard = !readScoped
	switch {
	case p.Request.Network || destructive:
		pol.Class = Destructive
		pol.Reason = "Destructive or network action requires approval"
	case process:
		pol.Class = Process
		pol.Reason = "Only an approved process snapshot may be signalled"
	case allRead && !readScoped:
		pol.Reason = "Read paths must resolve inside the project or scratch; explicit approval required"
	case allRead:
		pol.Class = Read
		pol.Reason = "Validated read-only command"
	}
	return pol, nil
}

func scopedReadPath(path, cwd string, s Scope) bool {
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	q, err := filepath.EvalSymlinks(path)
	return err == nil && (within(q, s.Project) || within(q, s.Scratch)) && !denied(q, s)
}

// Called only after readArgs validates the grammar. Option values and search
// patterns are not file operands. Git revisions before -- are not paths.
func readPaths(name string, args []string, cwd string) []string {
	var paths []string
	var operands string
	long := map[string]bool{}
	pattern := false
	explicitPattern := false
	gitRevisions := false
	switch name {
	case "ps", "pgrep", "uptime":
		return nil
	case "which":
		for _, a := range args {
			if strings.ContainsAny(a, "/\\") {
				paths = append(paths, a)
			}
		}
		return paths
	case "head", "tail":
		operands = "nc"
		long["--lines"] = true
		long["--bytes"] = true
	case "lsof":
		operands = "pi"
	case "du":
		operands = "d"
	case "rg", "grep":
		operands = "emg"
		long["--glob"] = true
		long["--max-count"] = true
		pattern = true
	case "find":
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				break
			}
			paths = append(paths, a)
		}
		return paths
	case "git":
		if args[0] == "branch" {
			return nil
		}
		gitRevisions = args[0] != "status"
		operands = "n"
		long["--max-count"] = true
		args = args[1:]
	}
	options := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		if options && a == "--" {
			options = false
			gitRevisions = false
			continue
		}
		if options && strings.HasPrefix(a, "-") && a != "-" {
			if strings.HasPrefix(a, "--") {
				if name == "rg" && a == "--files" {
					explicitPattern = true
				}
				key, _, eq := strings.Cut(a, "=")
				if long[key] && !eq {
					i++
				}
			} else {
				for j := 1; j < len(a); j++ {
					if strings.ContainsRune(operands, rune(a[j])) {
						if pattern && a[j] == 'e' {
							explicitPattern = true
						}
						if j == len(a)-1 {
							i++
						}
						break
					}
				}
			}
			continue
		}
		if gitRevisions && !strings.ContainsAny(a, "/\\") {
			if _, err := os.Lstat(filepath.Join(cwd, a)); os.IsNotExist(err) {
				continue
			}
		}
		paths = append(paths, a)
	}
	if pattern && !explicitPattern && len(paths) > 0 {
		paths = paths[1:]
	}
	if slices.Contains([]string{"cat", "head", "tail", "wc", "grep", "rg"}, name) {
		paths = slices.DeleteFunc(paths, func(path string) bool { return path == "-" })
	}
	return paths
}

// Extract only unambiguous literal nested argv. An unsupported wrapper grammar
// returns no candidates and still requires a card in classify.
func wrappedCommands(name string, args []string) [][]string {
	parseScript := func(script string) [][]string {
		if p, err := Parse(Request{Script: script}); err == nil {
			return p.Commands
		}
		return nil
	}
	var script string
	if name == "eval" {
		script = strings.Join(args, " ")
	} else if slices.Contains([]string{"sh", "bash", "zsh", "dash", "ksh", "fish", "csh", "tcsh"}, name) {
		for i, a := range args {
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c") && i+1 < len(args) {
				script = args[i+1]
				break
			}
		}
	}
	if script != "" {
		return parseScript(script)
	}
	switch name {
	case "flock":
		// The literal lockfile + command form. Options and fd-only forms
		// without a confidently identified command still require approval.
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		if len(args) < 2 || strings.HasPrefix(args[0], "-") {
			return nil
		}
		if len(args) == 3 && (args[1] == "-c" || args[1] == "--command") {
			return parseScript(args[2])
		}
		if strings.HasPrefix(args[1], "-") {
			return nil
		}
		return [][]string{args[1:]}
	case "script":
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "-q", "--quiet", "-e", "--return", "-f", "--flush":
				continue
			case "-c", "--command":
				if i+1 < len(args) && len(args[i+2:]) <= 1 {
					return parseScript(args[i+1])
				}
				return nil
			case "--":
				i++
			default:
				if strings.HasPrefix(args[i], "-") {
					return nil
				}
			}
			// BSD script takes an output file followed by literal argv.
			if i+1 < len(args) {
				return [][]string{args[i+1:]}
			}
			return nil
		}
		return nil
	case "watch":
		exec := false
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "-x", "--exec":
				exec = true
				continue
			case "-n", "--interval":
				i++
				continue
			case "-t", "--no-title", "-d", "--differences":
				continue
			case "--":
				i++
			default:
				if strings.HasPrefix(args[i], "-") {
					return nil
				}
			}
			if i >= len(args) {
				return nil
			}
			if exec {
				return [][]string{args[i:]}
			}
			return parseScript(strings.Join(args[i:], " "))
		}
		return nil
	case "parallel":
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return nil
		}
		for i, a := range args {
			if a == ":::" || a == "::::" {
				args = args[:i]
				break
			}
		}
		return parseScript(strings.Join(args, " "))
	}
	if name == "find" {
		var commands [][]string
		for i := 0; i < len(args); i++ {
			if slices.Contains([]string{"-exec", "-execdir", "-ok", "-okdir"}, args[i]) {
				start := i + 1
				i = start
				for i < len(args) && args[i] != ";" && args[i] != "+" {
					i++
				}
				if i > start {
					commands = append(commands, args[start:i])
				}
			}
		}
		return commands
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				return [][]string{args[i+1:]}
			}
			return nil
		}
		if name == "env" && strings.Contains(a, "=") && !strings.HasPrefix(a, "-") {
			continue
		}
		if name == "timeout" && !strings.HasPrefix(a, "-") {
			if i+1 < len(args) {
				return [][]string{args[i+1:]}
			}
			return nil
		}
		if strings.HasPrefix(a, "-") {
			switch {
			case name == "env" && (a == "-i" || a == "--ignore-environment"):
			case name == "unbuffer" && a == "-p", name == "time" && a == "-p":
			case name == "stdbuf" && slices.Contains([]string{"-i", "-o", "-e", "--input", "--output", "--error"}, a):
				i++
			case name == "stdbuf" && (len(a) > 2 && (strings.HasPrefix(a, "-i") || strings.HasPrefix(a, "-o") || strings.HasPrefix(a, "-e")) || strings.HasPrefix(a, "--input=") || strings.HasPrefix(a, "--output=") || strings.HasPrefix(a, "--error=")):
			case name == "env" && (a == "-u" || a == "--unset") || name == "nice" && (a == "-n" || a == "--adjustment") || name == "timeout" && slices.Contains([]string{"-s", "--signal", "-k", "--kill-after"}, a):
				i++
			case name == "nice" && (strings.HasPrefix(a, "-n") || strings.HasPrefix(a, "--adjustment=")):
			default:
				return nil
			}
			continue
		}
		switch name {
		case "env", "nice", "nohup", "command", "exec", "xargs", "builtin", "busybox", "toybox", "setsid", "time", "unbuffer", "stdbuf":
			return [][]string{args[i:]}
		}
		return nil
	}
	return nil
}

// Locate the subcommand before interpreting its options. Configuration and
// executable-path overrides can install helpers or aliases, so even a known
// subcommand needs approval with those options. Unknown grammars fail closed.
func gitSubcommand(args []string) (index int, approval bool) {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return i, approval
		}
		key, _, attached := strings.Cut(a, "=")
		switch key {
		case "--no-pager", "--paginate", "--no-optional-locks", "--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs", "--no-replace-objects", "--bare":
			if attached {
				return -1, true
			}
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env":
			if key == "-c" || key == "--config-env" {
				approval = true
			}
			if !attached {
				i++
				if i >= len(args) {
					return -1, true
				}
			}
		default:
			switch {
			case strings.HasPrefix(a, "-C") && len(a) > 2:
			case strings.HasPrefix(a, "-c") && len(a) > 2:
				approval = true
			default:
				return -1, true
			}
		}
	}
	return -1, true
}

// Each grammar explicitly names flags and their operands; unknown options do
// not inherit Read. '--' terminates options, preserving literal file names.
func flags(args []string, short, operand string, long map[string]bool) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return true
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			k, _, eq := strings.Cut(a, "=")
			takes, ok := long[k]
			if !ok || eq && !takes {
				return false
			}
			if takes && !eq {
				i++
				if i >= len(args) {
					return false
				}
			}
			continue
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.ContainsRune(operand, rune(c)) {
				if j == len(a)-1 {
					i++
					if i >= len(args) {
						return false
					}
				}
				break
			}
			if !strings.ContainsRune(short, rune(c)) {
				return false
			}
		}
	}
	return true
}
func readArgs(name string, a []string) bool {
	switch name {
	case "ls":
		return flags(a, "laAhldFtrSRn1", "", map[string]bool{"--all": false, "--long": false})
	case "cat":
		return flags(a, "benstuvETAn", "", nil)
	case "head", "tail":
		return flags(a, "qv", "nc", map[string]bool{"--lines": true, "--bytes": true})
	case "wc":
		return flags(a, "clmwL", "", nil)
	case "ps":
		return flags(a, "efalxuwA", "op", nil)
	case "pgrep":
		return flags(a, "flxan", "uP", nil)
	case "lsof":
		return flags(a, "nPl", "pi", nil)
	case "du":
		return flags(a, "shakcx", "d", nil)
	case "df":
		return flags(a, "hkiP", "", nil)
	case "uptime":
		return len(a) == 0
	case "rg":
		return flags(a, "nilwsvFcx", "emg", map[string]bool{"--files": false, "--hidden": false, "--glob": true, "--max-count": true})
	case "grep":
		return flags(a, "EinilwsvFcxrhH", "em", nil)
	case "stat":
		return flags(a, "fLt", "", nil)
	case "which":
		return flags(a, "a", "", nil)
	case "find":
		for i := 0; i < len(a); i++ {
			if !strings.HasPrefix(a[i], "-") {
				continue
			}
			switch a[i] {
			case "-type", "-name", "-iname", "-path", "-maxdepth", "-mindepth":
				i++
				if i >= len(a) {
					return false
				}
			case "-print":
			default:
				return false
			}
		}
		return true
	case "git":
		if len(a) == 0 {
			return false
		}
		switch a[0] {
		case "status":
			return flags(a[1:], "sbz", "", map[string]bool{"--short": false, "--branch": false, "--porcelain": false})
		case "log":
			return flags(a[1:], "p", "n", map[string]bool{"--oneline": false, "--stat": false, "--max-count": true})
		case "diff", "show":
			return flags(a[1:], "p", "", map[string]bool{"--stat": false, "--name-only": false, "--name-status": false, "--cached": false, "--staged": false})
		case "branch":
			return len(a) > 1 && a[1] == "--list" && flags(a[2:], "", "", nil)
		}
		return false
	}
	return false
}
