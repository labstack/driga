package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/secfile"
)

// launchctlTimeout bounds one launchctl call; bootout waits for the service
// to exit, which its graceful shutdown keeps well under this.
const launchctlTimeout = 30 * time.Second

// launchd finishes tearing a job down after bootout returns. Driga asks
// print every unloadPoll until the job is gone, for at most unloadWait, and
// retries a bootstrap launchd refuses while it is busy with backoff from
// unloadPoll, at most bootstrapAttempts times.
// A job Start finds loaded but not running may be one a bootout is still
// tearing down; Start watches it for settleWait before it kicks it.
const (
	unloadPoll        = 100 * time.Millisecond
	unloadWait        = 10 * time.Second
	settleWait        = time.Second
	bootstrapAttempts = 6
)

// ErrNoGUISession reports that the account has no gui/<uid> launchd domain:
// nobody is logged in to the desktop, as over SSH before any login. A
// LaunchAgent can run only in that domain; launchd loads its plist at the
// next login.
var ErrNoGUISession = errors.New("no macOS desktop session for this account; log in to the desktop session to start the Driga service")

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
	sleep     func(time.Duration)
	// refreshEnv records this process's environment in the plist. Without
	// it, a plist that exists keeps the environment it records: the app's
	// login-shell environment is the one source, and a terminal's install
	// does not replace it.
	refreshEnv bool
}

func newLaunchdController(binPath string) (Controller, error) {
	label := Label("darwin")
	return &launchdController{binPath: binPath, label: label,
		domain:    fmt.Sprintf("gui/%d", os.Getuid()),
		plistPath: filepath.Join(config.ExpandHome("~"), "Library", "LaunchAgents", label+".plist"),
		launchctl: runLaunchctl,
		sleep:     time.Sleep,
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

func (s *launchdController) refreshEnvironment() { s.refreshEnv = true }

// notLoaded reports launchctl's answers for a job the domain does not have:
// 3 (No such process) from bootout, 113 (Could not find service) from print
// and kickstart.
func notLoaded(code int) bool { return code == 3 || code == 113 }

// domainAbsent reports launchctl's answer for a gui/<uid> domain that does
// not exist: 112, Could not find domain.
func domainAbsent(code int, out string) bool {
	return code == 112 || strings.Contains(out, "Could not find domain")
}

// busy reports bootstrap's answers while launchd still tears down the job
// or another client bootstraps it: 5 (Input/output error), 36 (Operation
// now in progress) and 37 (Operation already in progress).
func busy(code int) bool { return code == 5 || code == 36 || code == 37 }

// runningState matches print's top-level state line; nested dictionaries
// are indented further.
var runningState = regexp.MustCompile(`(?m)^\tstate = running$`)

// state reports whether launchd has the job and whether its process runs.
// A job can be loaded but not running, for example after Driga.app moved
// and its executable vanished from under KeepAlive. Without a desktop
// session it returns ErrNoGUISession.
func (s *launchdController) state() (loaded, running bool, err error) {
	out, code, err := s.launchctl("print", s.target())
	switch {
	case err != nil:
		return false, false, err
	case domainAbsent(code, out):
		return false, false, ErrNoGUISession
	case notLoaded(code):
		return false, false, nil
	case code != 0:
		return false, false, launchctlError("print", code, out)
	}
	return true, runningState.MatchString(out), nil
}

func launchctlError(verb string, code int, out string) error {
	return fmt.Errorf("launchctl %s exited %d: %s", verb, code, strings.TrimSpace(out))
}

// bootout removes the job from launchd, stopping its process, and returns
// once launchd no longer has it, so a bootstrap that follows never meets
// the old definition. A job launchd does not have is already booted out.
func (s *launchdController) bootout() error {
	out, code, err := s.launchctl("bootout", s.target())
	switch {
	case err != nil:
		return err
	case domainAbsent(code, out), notLoaded(code):
		return nil
	case code != 0 && code != 36:
		return launchctlError("bootout", code, out)
	}
	for waited := time.Duration(0); ; waited += unloadPoll {
		loaded, _, err := s.state()
		if errors.Is(err, ErrNoGUISession) || err == nil && !loaded {
			return nil
		}
		if err != nil {
			return err
		}
		if waited >= unloadWait {
			return fmt.Errorf("launchd still has %s %s after bootout", s.label, unloadWait)
		}
		s.sleep(unloadPoll)
	}
}

// bootstrap loads the plist into the domain, which starts it (RunAtLoad).
// It retries while launchd is busy with the job; a job another client
// loaded meanwhile counts as loaded.
func (s *launchdController) bootstrap() error {
	delay := unloadPoll
	for attempt := 1; ; attempt++ {
		out, code, err := s.launchctl("bootstrap", s.domain, s.plistPath)
		switch {
		case err != nil || code == 0:
			return err
		case domainAbsent(code, out):
			return ErrNoGUISession
		case !busy(code):
			return launchctlError("bootstrap", code, out)
		}
		// Another client's bootstrap won: its job runs. A job that is
		// loaded but not running may still be on its way out.
		if loaded, running, err := s.state(); err != nil || loaded && running {
			return err
		}
		if attempt == bootstrapAttempts {
			return launchctlError("bootstrap", code, out)
		}
		s.sleep(delay)
		delay *= 2
	}
}

// installed reports a missing plist as ks.ErrNotInstalled.
func (s *launchdController) installed() error {
	if _, err := os.Stat(s.plistPath); errors.Is(err, os.ErrNotExist) {
		return ks.ErrNotInstalled
	} else if err != nil {
		return err
	}
	return nil
}

// Status is stopped for a plist launchd has not loaded. Without a desktop
// session it cannot tell, and says so.
func (s *launchdController) Status() (ks.Status, error) {
	loaded, running, err := s.state()
	switch {
	case errors.Is(err, ErrNoGUISession):
		if err := s.installed(); err != nil {
			return ks.StatusUnknown, err
		}
		return ks.StatusUnknown, ErrNoGUISession
	case err != nil:
		return ks.StatusUnknown, err
	case running:
		return ks.StatusRunning, nil
	case loaded:
		return ks.StatusStopped, nil
	}
	if err := s.installed(); err != nil {
		return ks.StatusUnknown, err
	}
	return ks.StatusStopped, nil
}

// Start makes repeating service install harmless when launchd already runs
// it: a running job is left alone, a loaded one that stays loaded is
// kicked, and an unloaded one is bootstrapped from its plist. A job Start
// bootstrapped starts by itself; Start never kicks a definition that a
// bootout is still tearing down.
func (s *launchdController) Start() error {
	loaded, running, err := s.state()
	if errors.Is(err, ErrNoGUISession) {
		if err := s.installed(); err != nil {
			return err
		}
		return ErrNoGUISession
	}
	if err != nil || running {
		return err
	}
	if loaded {
		if loaded, running, err = s.settle(); err != nil || running {
			return err
		}
		if loaded {
			return s.kickstart()
		}
	}
	if err := s.installed(); err != nil {
		return err
	}
	return s.bootstrap()
}

// settle watches a loaded job that does not run for settleWait, so Start
// bootstraps one that a bootout was tearing down instead of kicking it.
func (s *launchdController) settle() (loaded, running bool, err error) {
	for waited := time.Duration(0); ; waited += unloadPoll {
		s.sleep(unloadPoll)
		loaded, running, err = s.state()
		if errors.Is(err, ErrNoGUISession) {
			return false, false, err
		}
		if err != nil || !loaded || running || waited >= settleWait {
			return loaded, running, err
		}
	}
}

func (s *launchdController) kickstart(args ...string) error {
	out, code, err := s.launchctl(append(append([]string{"kickstart"}, args...), s.target())...)
	if err != nil || code == 0 {
		return err
	}
	return launchctlError("kickstart", code, out)
}

func (s *launchdController) Stop() error { return s.bootout() }

// Restart replaces a loaded job's process in place; an unloaded one starts.
func (s *launchdController) Restart() error {
	loaded, _, err := s.state()
	if err != nil && !errors.Is(err, ErrNoGUISession) {
		return err
	}
	if !loaded {
		return s.Start()
	}
	return s.kickstart("-k")
}

// Uninstall boots the job out and deletes its plist. Without a desktop
// session launchd has no job to boot out, and the plist is still deleted,
// so it does not load at the next login.
func (s *launchdController) Uninstall() error {
	loaded, _, err := s.state()
	if errors.Is(err, ErrNoGUISession) {
		loaded, err = false, nil
	} else if err == nil {
		err = s.bootout()
	}
	if err != nil {
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

// Install writes the launch agent's plist and loads it, or leaves both
// alone when the plist is already what this executable and environment
// would write. A plist that differs (another executable, a missing bundle
// association, an older definition, another data directory or a refreshed
// environment) is replaced: the new definition is rendered, checked and
// written beside the plist first, and only then is the job booted out (launchd
// keeps the definition it bootstrapped), the new plist renamed into place
// and bootstrapped. When launchd refuses it, the previous plist is restored
// and bootstrapped again. Without a desktop session the new plist stays for
// launchd to load at the next login.
func (s *launchdController) Install() error {
	if app := BundlePath(s.binPath); app != "" {
		for _, bundle := range []string{filepath.Join(app, "Contents", "Helpers", "Driga Service.app"), app} {
			if err := s.register(bundle); err != nil {
				return err
			}
		}
	}
	c := configForOS(s.binPath, "darwin")
	previous, err := os.ReadFile(s.plistPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var recorded map[string]string
	if previous != nil {
		if recorded, err = s.recordedEnvironment(); err != nil {
			slog.Warn("could not read the Driga service's environment; recording this one", "event", "service_env_unreadable", "err", err.Error())
		} else if !s.refreshEnv && len(recorded) > 0 {
			c.EnvVars = recorded
		}
	}
	data, err := renderLaunchd(c)
	if err != nil {
		return err
	}
	if bytes.Equal(previous, data) {
		return nil
	}
	if err := checkDefinition(data, s.label, c.Executable); err != nil {
		return fmt.Errorf("rendered service definition: %w", err)
	}
	if previous != nil {
		if keys := changedKeys(recorded, c.EnvVars); len(keys) > 0 {
			slog.Info("the Driga service's environment changed; restarting it", "event", "service_env_changed", "keys", strings.Join(keys, ","))
		}
		if dir := resolveDataDir(c.EnvVars["DRIGA_DATA_DIR"]); recorded != nil && resolveDataDir(recorded["DRIGA_DATA_DIR"]) != dir {
			slog.Info("the Driga service collected into another data directory; moving it to this one", "event", "service_data_dir_moved", "from", resolveDataDir(recorded["DRIGA_DATA_DIR"]), "to", dir)
		}
	}
	if err := secfile.MkdirPrivate(filepath.Dir(s.plistPath)); err != nil {
		return err
	}
	// A job can outlive its plist; boot it out even when no file exists.
	if err := secfile.WriteFileAtomic(s.plistPath, data, s.bootout); err != nil {
		return err
	}
	err = s.bootstrap()
	if err == nil || errors.Is(err, ErrNoGUISession) {
		return nil
	}
	if previous == nil {
		return err
	}
	restoreErr := secfile.WriteFileAtomic(s.plistPath, previous, nil)
	if restoreErr == nil {
		restoreErr = s.bootstrap()
	}
	if restoreErr != nil {
		return fmt.Errorf("the new service definition did not load: %w; restoring the previous one failed too: %w", err, restoreErr)
	}
	return fmt.Errorf("the new service definition did not load, so the previous one runs again: %w", err)
}

// changedKeys names the variables whose values differ between two
// environments; values are never logged.
func changedKeys(before, after map[string]string) []string {
	var keys []string
	for key, value := range after {
		if old, ok := before[key]; !ok || old != value {
			keys = append(keys, key)
		}
	}
	for key := range before {
		if _, ok := after[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// checkDefinition parses a rendered plist and checks that it names label and
// runs executable, before it replaces the one launchd runs.
func checkDefinition(data []byte, label, executable string) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, key, gotLabel, program := 0, "", "", ""
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.EndElement:
			depth--
		case xml.StartElement:
			depth++
			// plist > dict > the dict's keys and values
			if depth != 3 {
				continue
			}
			var value struct {
				Text    string   `xml:",chardata"`
				Strings []string `xml:"string"`
			}
			switch {
			case t.Name.Local == "key", key == "Label" && t.Name.Local == "string", key == "ProgramArguments" && t.Name.Local == "array":
				if err := decoder.DecodeElement(&value, &t); err != nil {
					return err
				}
				depth--
			default:
				continue
			}
			switch t.Name.Local {
			case "key":
				key = value.Text
			case "string":
				gotLabel = value.Text
			case "array":
				if len(value.Strings) > 0 {
					program = value.Strings[0]
				}
			}
		}
	}
	if depth != 0 || gotLabel != label || program != executable {
		return fmt.Errorf("label %q runs %q; want %q running %q", gotLabel, program, label, executable)
	}
	return nil
}
