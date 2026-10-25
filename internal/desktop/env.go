package desktop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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

// loginEnv is the login environment collector starts use. Each launch reads
// the login shell again in the background, so a PATH changed since the last
// launch takes effect; until that capture ends, the one an earlier launch
// stored in the data directory serves.
type loginEnv struct {
	path    string
	capture func(context.Context) (map[string]string, error)
	// done closes once this launch's capture ended.
	done  chan struct{}
	mu    sync.Mutex
	fresh map[string]string
	err   error
}

func newLoginEnv(dataDir string, capture func(context.Context) (map[string]string, error)) *loginEnv {
	return &loginEnv{path: filepath.Join(dataDir, "app-env.json"), capture: capture, done: make(chan struct{})}
}

// refresh captures the login environment and stores it for the next launch.
// It runs once per launch.
func (e *loginEnv) refresh(ctx context.Context) {
	defer close(e.done)
	env, err := e.capture(ctx)
	if err == nil {
		if werr := e.store(env); werr != nil {
			slog.Warn("could not store the login environment", "event", "login_env_store_failed", "err", werr.Error())
		}
	}
	e.mu.Lock()
	e.fresh, e.err = env, err
	e.mu.Unlock()
}

func (e *loginEnv) store(env map[string]string) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if err := secfile.MkdirPrivate(filepath.Dir(e.path)); err != nil {
		return err
	}
	return secfile.WritePrivate(e.path, b)
}

// get returns this launch's capture once it ended, else the stored
// environment. With neither, as on the first launch, it waits for the capture.
func (e *loginEnv) get(ctx context.Context) (map[string]string, error) {
	select {
	case <-e.done:
		e.mu.Lock()
		fresh, err := e.fresh, e.err
		e.mu.Unlock()
		if err == nil {
			return fresh, nil
		}
		if stored, serr := e.stored(); serr == nil {
			return stored, nil
		}
		return nil, err
	default:
	}
	stored, err := e.stored()
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	select {
	case <-e.done:
		return e.get(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *loginEnv) stored() (map[string]string, error) {
	b, err := os.ReadFile(e.path)
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("read %s: %w", e.path, err)
	}
	return env, nil
}

// childEnvironment merges the login environment over a minimal base taken
// from the app's own environment. Windows children inherit the session
// environment whole, as any program started from the shell does. The child's
// DRIGA_* variables are exactly the app's own, without DRIGA_APP: they name
// the data, config and settings the app shows, which its collector must use,
// so a login shell's never reach it.
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
	for key := range env {
		if strings.HasPrefix(key, "DRIGA_") {
			delete(env, key)
		}
	}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(key, "DRIGA_") && key != "DRIGA_APP" {
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

// shadowedKeys returns, sorted, the DRIGA_* variables the login environment
// sets to another value than the app's own. The collector uses the app's, so
// a data directory exported in a shell profile is not the one the app shows.
func shadowedKeys(login map[string]string, getenv func(string) string) []string {
	var keys []string
	for key, value := range login {
		if strings.HasPrefix(key, "DRIGA_") && getenv(key) != value {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}
