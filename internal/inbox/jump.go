package inbox

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

type JumpOptions struct {
	DrigaPath string
	Tmux      string
	Terminal  bool
	OS        string
	Procs     func(context.Context) ([]model.Proc, error) // Optional process snapshot override for fixtures.
}

// RunJump follows a local destination. Callers must reject remote items first.
// Outside tmux, select the most recently active attached client explicitly.
func RunJump(ctx context.Context, r Runner, j *model.InboxJump, opts JumpOptions) (string, error) {
	commands, err := JumpCommands(j, opts)
	if err != nil {
		return "", err
	}
	var notice, tty string
	if len(commands) > 0 && j.Kind == "tmux" && opts.Tmux == "" {
		out, err := r.Run(ctx, "tmux", "-S", j.Server, "list-clients", "-F", "#{client_activity} #{client_name} #{client_tty}")
		if err != nil {
			return "", err
		}
		var client string
		latest := int64(-1)
		for _, line := range strings.Split(string(out), "\n") {
			// Preserve the final separator: control clients may have no TTY.
			activity, rest, ok := strings.Cut(strings.TrimLeft(strings.TrimSuffix(line, "\r"), " \t"), " ")
			n, err := strconv.ParseInt(activity, 10, 64)
			sep := strings.LastIndexByte(rest, ' ')
			if !ok || err != nil || n < 0 || sep < 0 {
				continue
			}
			name := strings.TrimSpace(rest[:sep])
			if name != "" && n > latest {
				latest, client, tty = n, name, strings.TrimSpace(rest[sep+1:])
			}
		}
		if client == "" {
			notice = "no tmux client is attached"
		} else {
			commands = append([][]string{{"tmux", "-S", j.Server, "switch-client", "-c", client, "-t", j.Target}}, commands...)
		}
	}
	for _, args := range commands {
		if _, err := r.Run(ctx, args[0], args[1:]...); err != nil {
			return notice, err
		}
	}
	goos := opts.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "darwin" && tty != "" {
		procs := opts.Procs
		if procs == nil {
			procs = platform.New().Procs
		}
		if snapshot, err := procs(ctx); err == nil {
			if app := terminalApp(snapshot, tty); app != "" {
				// An alert click is an explicit user action, so activating its
				// terminal does not require the inbox.jump_terminal opt-in.
				if _, err := r.Run(ctx, "open", "-a", app); err != nil {
					return notice, err
				}
			}
		}
	}
	return notice, nil
}

// terminalApp finds an app bundle through the client's controlling TTY and
// parent chain. Terminal apps themselves often have no controlling TTY.
func terminalApp(procs []model.Proc, tty string) string {
	if tty == "" {
		return ""
	}
	byPID := make(map[int32]model.Proc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	for _, p := range procs {
		if p.TTY != tty || p.Zombie {
			continue
		}
		seen := map[int32]bool{}
		for p.PID > 0 && !seen[p.PID] && !p.Zombie {
			seen[p.PID] = true
			if bundle := derive.AppBundle(p.Exe); bundle != "" {
				return bundle
			}
			p = byPID[p.PPID]
		}
	}
	return ""
}

// JumpCommands plans only proven local destinations. Callers must reject remote
// items before execution. Resume commands remain copyable text, never executed.
func JumpCommands(j *model.InboxJump, opts JumpOptions) ([][]string, error) {
	if j == nil {
		return nil, fmt.Errorf("item has no jump")
	}
	switch j.Kind {
	case "machine":
		bin := opts.DrigaPath
		if bin == "" {
			bin = "driga"
		}
		goos := opts.OS
		if goos == "" {
			goos = runtime.GOOS
		}
		if goos == "darwin" {
			const script = "on run argv\ntell application \"Terminal\"\nactivate\ndo script (item 1 of argv)\nend tell\nend run"
			return [][]string{{"osascript", "-e", script, Quote(bin) + " top --tab machine"}}, nil
		}
		if goos == "linux" {
			return [][]string{{"x-terminal-emulator", "-e", bin, "top", "--tab", "machine"}}, nil
		}
		return nil, fmt.Errorf("machine jump unsupported on %s", goos)
	case "tmux":
		if j.Target != "" && j.Server != "" {
			prefix := []string{"tmux", "-S", j.Server}
			var commands [][]string
			server, _, _ := strings.Cut(opts.Tmux, ",")
			if server == j.Server {
				commands = append(commands, append(append([]string{}, prefix...), "switch-client", "-t", j.Target))
			}
			commands = append(commands, append(prefix, "select-pane", "-t", j.Target))
			return commands, nil
		}
	case "terminal":
		goos := opts.OS
		if goos == "" {
			goos = runtime.GOOS
		}
		if opts.Terminal && goos == "darwin" && j.Target != "" {
			if script := TerminalActivateScript(j.App); script != "" {
				return [][]string{{"osascript", "-e", script, j.Target}}, nil
			}
		}
	case "resume":
	default:
		return nil, fmt.Errorf("item has no jump")
	}
	if j.Command == "" {
		return nil, fmt.Errorf("item has no jump")
	}
	return nil, nil
}

// JumpCommand includes the working directory when offering a fallback to copy.
func JumpCommand(j *model.InboxJump) string {
	if j == nil || j.Command == "" {
		return ""
	}
	if j.Cwd != "" {
		return "cd -- " + Quote(j.Cwd) + " && " + j.Command
	}
	return j.Command
}
