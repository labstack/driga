package desktop

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/service"
)

// replacedMarker, in the data directory, records that the app removed the
// background service, so it does so, and says so, once.
const replacedMarker = "service-replaced"

// serviceReplacedMessage is the app's one notice that it took over collection.
const serviceReplacedMessage = "Driga now collects in the app; the background service was removed."

// ReplaceService reports whether the app removes the per-user collector
// service: on macOS and Windows, where the app collects, when one is
// installed and the app has not removed one before. A Linux unit may be a
// headless setup, which the best-effort desktop app leaves alone.
func ReplaceService(goos string, installed, done bool) bool {
	return (goos == "darwin" || goos == "windows") && installed && !done
}

// installedService is the per-user collector service as the app sees it.
type installedService interface {
	Installed() (bool, error)
	Uninstall() error
}

// userService is the launchd agent or Task Scheduler task that driga service
// install registers.
type userService struct{ service.Controller }

func (s userService) Installed() (bool, error) { return service.Installed(s.Controller) }

// openUserService opens the service registered for the executable exe.
func openUserService(exe string) func() (installedService, error) {
	return func() (installedService, error) {
		c, err := service.New(exe)
		if err != nil {
			return nil, err
		}
		return userService{c}, nil
	}
}

// replaceService uninstalls the per-user service when ReplaceService says so,
// the way driga service uninstall does, and records it. It reports whether it
// removed one.
func replaceService(goos, dataDir string, open func() (installedService, error)) (bool, error) {
	marker := filepath.Join(dataDir, replacedMarker)
	_, err := os.Stat(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	done := err == nil
	// Ask the service manager only where the answer could matter.
	if !ReplaceService(goos, true, done) {
		return false, nil
	}
	svc, err := open()
	if err != nil {
		return false, err
	}
	installed, err := svc.Installed()
	if err != nil || !ReplaceService(goos, installed, done) {
		return false, err
	}
	if err := svc.Uninstall(); err != nil {
		return false, err
	}
	if err := secfile.MkdirPrivate(dataDir); err != nil {
		return true, err
	}
	return true, secfile.WritePrivate(marker, nil)
}
