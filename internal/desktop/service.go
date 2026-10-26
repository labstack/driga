package desktop

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/labstack/driga/internal/service"
)

// The app does not collect. The Driga service, drigad, does: it runs as a
// login service whether the app is open or not, and the app is one of its
// clients. On launch the app installs or starts it with the login shell's
// environment, and restarts it when it runs another version than the app.

// servicePoll is how often the app checks that the service runs.
const servicePoll = 2 * time.Second

// ensureService supplies the app environment; lifecycle owns startup decisions.
func (s *shell) ensureService(ctx context.Context) error {
	home, account := s.homes()
	s.lifecycle = service.NewLifecycle(service.LifecycleOptions{
		Executable: s.serviceExe, DataDir: s.dataDir, Version: s.opts.Version,
		Platform: s.goos, Home: home, AccountHome: account,
		Environment: childEnvironment(s.serviceEnv(ctx)),
	})
	return s.lifecycle.Ensure(ctx)
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

// watchService keeps the tray's status line on the service: installs it
// once, then checks that it runs.
func (s *shell) watchService(ctx context.Context) {
	err := s.ensureService(ctx)
	if errors.Is(err, service.ErrIsolatedHome) {
		slog.Info("the app runs in an isolated environment; leaving the login service alone", "event", "service_isolated")
		err = nil
	} else if err != nil {
		slog.Warn("could not start the Driga service", "event", "service_start_failed", "err", err.Error())
	}
	ticker := time.NewTicker(servicePoll)
	defer ticker.Stop()
	for {
		st := s.lifecycle.Observe()
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
