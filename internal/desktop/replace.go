package desktop

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"

	"github.com/labstack/driga/internal/pathx"
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
	// DataDir is the data directory the service collects into.
	DataDir() (string, error)
	Uninstall() error
}

// userService is the launchd agent or Task Scheduler task that driga service
// install registers.
type userService struct{ service.Controller }

func (s userService) Installed() (bool, error) { return service.Installed(s.Controller) }
func (s userService) DataDir() (string, error) { return service.DataDir(s.Controller) }

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

// errIsolatedHome reports an app whose HOME is not the account's home.
var errIsolatedHome = errors.New("HOME is not the account's home; the background service is left alone")

// accountHomes returns HOME and the account's home directory.
func accountHomes() (env, account string) {
	if u, err := user.Current(); err == nil {
		account = u.HomeDir
	}
	return os.Getenv("HOME"), account
}

// isolatedHome reports whether HOME is set to another directory than the
// account's home, symlinks resolved. An unset HOME, as on Windows, is the
// account's home.
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

// replaceService uninstalls the per-user service when ReplaceService says so,
// the way driga service uninstall does, and records it. It reports whether it
// removed one. The service manager knows one service per user, so a service
// that collects into another data directory, as the user's own does for an
// app run with DRIGA_DATA_DIR, is left alone. So is every service when HOME
// is not the account's home: the app would read the data directory from a
// plist under HOME while launchd finds the account's own, so it does not ask
// at all and returns errIsolatedHome.
func replaceService(goos, dataDir string, homes func() (string, string), open func() (installedService, error)) (bool, error) {
	if isolatedHome(homes) {
		return false, errIsolatedHome
	}
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
	serviceDir, err := svc.DataDir()
	if err != nil {
		return false, err
	}
	if !pathx.Equal(serviceDir, dataDir) {
		return false, nil
	}
	if err := svc.Uninstall(); err != nil {
		return false, err
	}
	if err := secfile.MkdirPrivate(dataDir); err != nil {
		return true, err
	}
	return true, secfile.WritePrivate(marker, nil)
}
