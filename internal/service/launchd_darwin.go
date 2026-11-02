package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/secfile"
)

// launchctlTimeout bounds one launchctl call; bootout waits for the service
// to exit, which its graceful shutdown keeps well under this.
const launchctlTimeout = 30 * time.Second

// launchctl runs /bin/launchctl and returns its combined output and exit
// code. err reports only a launchctl that could not run.
type launchctl func(args ...string) (out string, code int, err error)

func runLaunchctl(args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), launchctlTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		return string(out), exit.ExitCode(), nil
	}
	if err != nil {
		return string(out), -1, fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
	}
	return string(out), 0, nil
}

// launchdController drives the per-user launch agent through launchctl's
// domain commands (bootstrap, bootout, kickstart, print), never the
// deprecated load and unload, which refuse a job launchd already has.
type launchdController struct {
	binPath   string
	label     string
	domain    string // gui/<uid>
	plistPath string
	launchctl launchctl
	register  func(string) error
}

func newLaunchdController(binPath string) (Controller, error) {
	label := Label("darwin")
	return &launchdController{binPath: binPath, label: label,
		domain:    fmt.Sprintf("gui/%d", os.Getuid()),
		plistPath: filepath.Join(config.ExpandHome("~"), "Library", "LaunchAgents", label+".plist"),
		launchctl: runLaunchctl,
		register: func(app string) error {
			// Apple requires Launch Services to know the associated bundle,
			// wherever Driga.app was installed.
			out, err := exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", app).CombinedOutput()
			if err != nil {
				return fmt.Errorf("register Driga.app: %w: %s", err, out)
			}
			return nil
		},
	}, nil
}

// recordedEnvironment is the environment the plist gives the collector.
func (s *launchdController) recordedEnvironment() (map[string]string, error) {
	return plistEnvironment(s.plistPath)
}

func (s *launchdController) target() string { return s.domain + "/" + s.label }

// recordedDataDir is the DRIGA_DATA_DIR the plist gives the collector.
func (s *launchdController) recordedDataDir() (string, error) {
	return plistEnv(s.plistPath, "DRIGA_DATA_DIR")
}

// notLoaded reports launchctl's answers for a job the domain does not have:
// 3 (No such process) from bootout, 113 (Could not find service) from print
// and kickstart.
func notLoaded(code int) bool { return code == 3 || code == 113 }

// runningState matches print's top-level state line; nested dictionaries
// are indented further.
var runningState = regexp.MustCompile(`(?m)^\tstate = running$`)

// state reports whether launchd has the job and whether its process runs.
// A job can be loaded but not running, for example after Driga.app moved
// and its executable vanished from under KeepAlive.
func (s *launchdController) state() (loaded, running bool, err error) {
	out, code, err := s.launchctl("print", s.target())
	if err != nil {
		return false, false, err
	}
	if notLoaded(code) {
		return false, false, nil
	}
	if code != 0 {
		return false, false, launchctlError("print", code, out)
	}
	return true, runningState.MatchString(out), nil
}

func launchctlError(verb string, code int, out string) error {
	return fmt.Errorf("launchctl %s exited %d: %s", verb, code, strings.TrimSpace(out))
}

// bootout removes the job from launchd, stopping its process. A job launchd
// does not have is already booted out.
func (s *launchdController) bootout() error {
	out, code, err := s.launchctl("bootout", s.target())
	if err != nil || code == 0 || notLoaded(code) {
		return err
	}
	return launchctlError("bootout", code, out)
}

func (s *launchdController) Status() (ks.Status, error) {
	loaded, running, err := s.state()
	switch {
	case err != nil:
		return ks.StatusUnknown, err
	case running:
		return ks.StatusRunning, nil
	case loaded:
		return ks.StatusStopped, nil
	}
	if _, err := os.Stat(s.plistPath); err == nil {
		return ks.StatusStopped, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return ks.StatusUnknown, err
	}
	return ks.StatusUnknown, ks.ErrNotInstalled
}

// Start makes repeating service install harmless when launchd already runs
// it: a running job is left alone, a loaded one is kicked, and an unloaded
// one is bootstrapped from its plist.
func (s *launchdController) Start() error {
	loaded, running, err := s.state()
	if err != nil || running {
		return err
	}
	if !loaded {
		if _, err := os.Stat(s.plistPath); errors.Is(err, os.ErrNotExist) {
			return ks.ErrNotInstalled
		} else if err != nil {
			return err
		}
		out, code, err := s.launchctl("bootstrap", s.domain, s.plistPath)
		if err != nil {
			return err
		}
		if code == 0 {
			return nil
		}
		// Another client may have bootstrapped it meanwhile (launchctl says
		// 5: Input/output error, or 37: Operation already in progress).
		if loaded, running, err = s.state(); err != nil || running {
			return err
		}
		if !loaded {
			return launchctlError("bootstrap", code, out)
		}
	}
	out, code, err := s.launchctl("kickstart", s.target())
	if err != nil || code == 0 {
		return err
	}
	return launchctlError("kickstart", code, out)
}

func (s *launchdController) Stop() error { return s.bootout() }

// Restart replaces a loaded job's process in place; an unloaded one starts.
func (s *launchdController) Restart() error {
	loaded, _, err := s.state()
	if err != nil {
		return err
	}
	if !loaded {
		return s.Start()
	}
	out, code, err := s.launchctl("kickstart", "-k", s.target())
	if err != nil || code == 0 {
		return err
	}
	return launchctlError("kickstart", code, out)
}

func (s *launchdController) Uninstall() error {
	loaded, _, err := s.state()
	if err != nil {
		return err
	}
	if err := s.bootout(); err != nil {
		return err
	}
	err = os.Remove(s.plistPath)
	if errors.Is(err, os.ErrNotExist) {
		if loaded {
			return nil
		}
		return ks.ErrNotInstalled
	}
	return err
}

// Install writes the launch agent's plist, or leaves it alone when it is
// already what this executable and environment would write. A plist that
// differs (another executable, a missing bundle association, an older
// definition, another data directory or a changed login environment) is
// replaced. launchd keeps the definition it bootstrapped, so the job is booted
// out first whether or not its process runs; Start bootstraps the new one.
func (s *launchdController) Install() error {
	if app := BundlePath(s.binPath); app != "" {
		for _, bundle := range []string{filepath.Join(app, "Contents", "Helpers", "Driga Service.app"), app} {
			if err := s.register(bundle); err != nil {
				return err
			}
		}
	}
	data, err := renderLaunchd(configForOS(s.binPath, "darwin"))
	if err != nil {
		return err
	}
	current, err := os.ReadFile(s.plistPath)
	if err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if dir, err := s.recordedDataDir(); err == nil && resolveDataDir(dir) != config.DataDir() {
			slog.Info("the Driga service collected into another data directory; moving it to this one", "event", "service_data_dir_moved", "from", resolveDataDir(dir), "to", config.DataDir())
		}
	}
	// A job can outlive its plist; boot it out even when no file exists.
	if err := s.bootout(); err != nil {
		return err
	}
	if err := secfile.MkdirPrivate(filepath.Dir(s.plistPath)); err != nil {
		return err
	}
	return secfile.WriteFileAtomic(s.plistPath, data, nil)
}
