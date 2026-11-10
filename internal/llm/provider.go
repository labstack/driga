package llm

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/labstack/driga/internal/platform"
)

// RunProvider owns the lifetime of a pinned assistant runtime. Inherited
// conversation authority is removed; callers supply explicit tool bridge scope.
func RunProvider(ctx context.Context, path, dir string, args, extraEnv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, stdin, stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if key == "DRIGA_INTERNAL" || strings.HasPrefix(key, "DRIGA_CHAT_") ||
			strings.HasPrefix(key, "CODEX_EXEC_SERVER_") ||
			strings.HasPrefix(key, "COPILOT_PROVIDER_") || key == "COPILOT_PROVIDERS_CONFIG" ||
			key == "COPILOT_ALLOW_ALL" || key == "COPILOT_CUSTOM_INSTRUCTIONS_DIRS" ||
			key == "COPILOT_SKILLS_DIRS" || key == "BASH_ENV" || key == "ENV" ||
			key == "NODE_OPTIONS" || key == "NODE_PATH" || key == "COPILOT_AUTO_UPDATE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "DRIGA_INTERNAL=1", "COPILOT_AUTO_UPDATE=false")
	cmd.Env = append(cmd.Env, extraEnv...)
	return platform.RunChild(cmd)
}
