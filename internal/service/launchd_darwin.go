package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/secfile"
)

type launchdController struct {
	Controller
	binPath   string
	plistPath string
	register  func(string) error
}

func newLaunchdController(controller Controller, binPath string) *launchdController {
	return &launchdController{Controller: controller, binPath: binPath,
		plistPath: filepath.Join(config.ExpandHome("~"), "Library", "LaunchAgents", "run.driga.collector.plist"),
		register: func(app string) error {
			// Caskroom isn't automatically scanned like /Applications. Apple
			// requires Launch Services to know the associated bundle.
			out, err := exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", app).CombinedOutput()
			if err != nil {
				return fmt.Errorf("register Driga.app: %w: %s", err, out)
			}
			return nil
		},
	}
}

// Start makes repeating service install harmless when launchd already runs it.
func (s *launchdController) Start() error {
	status, err := s.Controller.Status()
	if err == nil && status == ks.StatusRunning {
		return nil
	}
	if err != nil && !errors.Is(err, ks.ErrNotInstalled) {
		return err
	}
	return s.Controller.Start()
}

func (s *launchdController) Install() error {
	if app := BundlePath(s.binPath); app != "" {
		if err := s.register(app); err != nil {
			return err
		}
	}
	_, err := os.Stat(s.plistPath)
	if err == nil {
		stale, err := PlistStale(s.plistPath, s.binPath)
		if err != nil {
			return err
		}
		if !stale {
			return nil
		}
		status, err := s.Status()
		if err != nil && !errors.Is(err, ks.ErrNotInstalled) {
			return err
		}
		if status == ks.StatusRunning {
			if err := s.Stop(); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := renderLaunchd(configForOS(s.binPath, "darwin"))
	if err != nil {
		return err
	}
	if err := secfile.MkdirPrivate(filepath.Dir(s.plistPath)); err != nil {
		return err
	}
	// Atomic replacement retains the old plist if writing the new one fails.
	file, err := secfile.CreateTemp(filepath.Dir(s.plistPath), ".driga-plist-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.plistPath)
}
