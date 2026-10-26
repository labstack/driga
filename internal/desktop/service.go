package desktop

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/service"
)

// The app does not collect. The Driga service, drigad, does: it runs as a
// login service whether the app is open or not, and the app is one of its
// clients. On launch the app installs or starts it with the login shell's
// environment, and restarts it when it runs another version than the app.

// servicePoll is how often the app checks that the service runs.
const servicePoll = 2 * time.Second

// Status is the Driga service as the app sees it.
type Status struct {
	// Running is true while the service holds its lock.
	Running bool
	Owner   collector.Owner
	Err     error
	// RestartPending is the app's: an update waits for Driga to restart.
	RestartPending bool
}

// errIsolatedHome reports an app whose HOME is not the account's home.
var errIsolatedHome = errors.New("HOME is not the account's home; the login service is left alone")

// ensureService installs the login service, or starts it, and restarts it
// when it runs another version than this app. It reports why it could not.
func (s *shell) ensureService(ctx context.Context) error {
	if isolatedHome(s.homes) {
		return errIsolatedHome
	}
	env := s.serviceEnv(ctx)
	return startService(s.goos, s.opts.Version, func() (bool, error) {
		controller, err := service.New(s.serviceExe)
		if err != nil {
			return false, err
		}
		return service.Installed(controller)
	}, func(action string) error {
		return s.runService(ctx, env, action)
	}, func() (collector.Owner, bool, error) { return collector.ReadOwner(s.dataDir) })
}

// startService preserves macOS's plist refresh, while managers that reject an
// existing registration start it directly. Version changes still restart it.
func startService(goos, version string, installed func() (bool, error), run func(string) error, readOwner func() (collector.Owner, bool, error)) error {
	action := "install"
	if goos != "darwin" {
		exists, err := installed()
		if err != nil {
			return fmt.Errorf("query installed Driga service: %w", err)
		}
		if exists {
			action = "start"
		}
	}
	if err := run(action); err != nil {
		return err
	}
	owner, held, err := readOwner()
	if err != nil || !held || owner.Version == "" || owner.Version == version {
		return err
	}
	slog.Info("the Driga service runs another version; restarting it", "event", "service_restart", "running", owner.Version, "app", version)
	return run("restart")
}

// serviceEnv is the login shell's environment, which the login service is
// installed with, so it finds tmux and the agent CLIs a Finder launch lacks.
func (s *shell) serviceEnv(ctx context.Context) map[string]string {
	if s.loginEnv == nil {
		return nil
	}
	env, err := s.loginEnv.get(ctx)
	shadowed := shadowedKeys(env, os.Getenv)
	s.mu.Lock()
	s.envErr, s.shadowed = err, shadowed
	s.mu.Unlock()
	if err != nil {
		slog.Warn("login environment unavailable", "event", "login_env_failed", "err", err.Error())
	}
	if len(shadowed) > 0 {
		slog.Warn("the login shell sets DRIGA_* variables the app does not use", "event", "login_env_shadowed", "keys", strings.Join(shadowed, ","))
	}
	return env
}

// runService runs `driga service <action>` with the login environment, as a
// person would from their shell.
func (s *shell) runService(ctx context.Context, env map[string]string, action string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.serviceExe, "service", action)
	cmd.Env = childEnvironment(env)
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(bytes.TrimSpace(out))), "\n")
		return fmt.Errorf("driga service %s: %s", action, cmp.Or(lines[len(lines)-1], err.Error()))
	}
	return nil
}

// watchService keeps the tray's status line on the service: installs it
// once, then checks that it runs.
func (s *shell) watchService(ctx context.Context) {
	err := s.ensureService(ctx)
	if errors.Is(err, errIsolatedHome) {
		slog.Info("the app runs in an isolated environment; leaving the login service alone", "event", "service_isolated")
		err = nil
	} else if err != nil {
		slog.Warn("could not start the Driga service", "event", "service_start_failed", "err", err.Error())
	}
	s.mu.Lock()
	s.serviceErr = err
	s.mu.Unlock()
	ticker := time.NewTicker(servicePoll)
	defer ticker.Stop()
	for {
		owner, held, readErr := collector.ReadOwner(s.dataDir)
		st := Status{Running: held && collector.Running(s.dataDir), Owner: owner, Err: readErr}
		s.mu.Lock()
		changed := st.Running != s.status.Running || st.Owner != s.status.Owner || (st.Err == nil) != (s.status.Err == nil)
		s.status = st
		s.mu.Unlock()
		if changed {
			s.updateTray()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// accountHomes returns HOME and the account's home directory.
func accountHomes() (env, account string) {
	if u, err := user.Current(); err == nil {
		account = u.HomeDir
	}
	return os.Getenv("HOME"), account
}

// isolatedHome reports whether HOME is set to another directory than the
// account's home, symlinks resolved. An unset HOME, as on Windows, is the
// account's home. The service manager knows one service per account, so an
// app run with another HOME leaves it alone.
func isolatedHome(homes func() (string, string)) bool {
	env, account := homes()
	if env == "" {
		return false
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	return account == "" || !pathx.Equal(resolve(env), resolve(account))
}
