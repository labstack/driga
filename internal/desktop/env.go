package desktop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/service"
)

const loginShellTimeout = 5 * time.Second

// baseKeys are kept from the app's own environment: the session identity and
// locale. Everything the collector needs from the user's shell comes from the
// login environment instead.
var baseKeys = []string{"HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "PATH"}

// LoginEnvironment runs the user's login shell and returns the variables a
// collector started outside that shell needs, the same ones the background
// service records. Apps launched from Finder or a desktop session get a bare
// PATH, so tmux and agent CLIs would not be found without it. Windows has no
// login shell and returns nothing.
func LoginEnvironment(ctx context.Context) (map[string]string, error) {
	if runtime.GOOS == "windows" {
		return map[string]string{}, nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		return nil, errors.New("SHELL is not set")
	}
	ctx, cancel := context.WithTimeout(ctx, loginShellTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", "env")
	// A profile can start background programs that keep stdout open.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read login environment from %s: %w", shell, err)
	}
	keys := service.EnvKeys()
	env := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && value != "" && slices.Contains(keys, key) {
			env[key] = value
		}
	}
	return env, scanner.Err()
}

// LoadEnvironment returns the login environment stored in dataDir, capturing
// and storing it on first use. Running the login shell is slow and can show
// prompts from the user's profile, so it happens once.
func LoadEnvironment(ctx context.Context, dataDir string) (map[string]string, error) {
	path := filepath.Join(dataDir, "app-env.json")
	if b, err := os.ReadFile(path); err == nil {
		env := map[string]string{}
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		return env, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	env, err := LoginEnvironment(ctx)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	if err := secfile.MkdirPrivate(dataDir); err != nil {
		return nil, err
	}
	if err := secfile.WritePrivate(path, b); err != nil {
		return nil, err
	}
	return env, nil
}

// childEnvironment merges the login environment over a minimal base taken
// from the app's own environment. Windows children inherit the session
// environment whole, as any program started from the shell does. The app's
// own DRIGA_* variables win over the login ones: they name the data and
// config the app shows, which its collector must write.
func childEnvironment(login map[string]string) []string {
	env := map[string]string{}
	if runtime.GOOS == "windows" {
		for _, entry := range os.Environ() {
			if key, value, ok := strings.Cut(entry, "="); ok && key != "" {
				env[key] = value
			}
		}
	} else {
		for _, key := range baseKeys {
			if value := os.Getenv(key); value != "" {
				env[key] = value
			}
		}
	}
	for key, value := range login {
		env[key] = value
	}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(key, "DRIGA_") {
			env[key] = value
		}
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	slices.Sort(out)
	return out
}
