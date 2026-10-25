package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"
)

const upgradeInterval = time.Minute

// upgradeWatch notices when a package manager replaces the executable with
// another version, so the app restarts before an old app reads a store a
// newer collector migrated.
type upgradeWatch struct {
	exe     string
	version string
	// interval defaults to upgradeInterval.
	interval time.Duration
	stat     func(path string) (time.Time, error)
	read     func(ctx context.Context, exe string) (string, error)
	stamp    time.Time
}

func newUpgradeWatch(exe, version string) *upgradeWatch {
	return &upgradeWatch{exe: exe, version: version, stat: modTime, read: exeVersion}
}

// start records the executable as it is now.
func (w *upgradeWatch) start() { w.stamp, _ = w.stat(w.exe) }

// changed reports whether the executable now holds another version. It asks
// the executable only after its modification time moved, and asks again on
// the next check while it cannot answer, as during an upgrade.
func (w *upgradeWatch) changed(ctx context.Context) bool {
	stamp, err := w.stat(w.exe)
	if err != nil || stamp.Equal(w.stamp) {
		return false
	}
	version, err := w.read(ctx, w.exe)
	if err != nil {
		return false
	}
	if version != w.version {
		return true
	}
	w.stamp = stamp
	return false
}

// run checks every minute and calls upgraded on a new version until it
// reports that the app restarts. A refused restart is asked again on the
// next check.
func (w *upgradeWatch) run(ctx context.Context, upgraded func() bool) {
	w.start()
	interval := w.interval
	if interval <= 0 {
		interval = upgradeInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if w.changed(ctx) && upgraded() {
				return
			}
		}
	}
}

func modTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// exeVersion asks a driga executable for its version.
func exeVersion(ctx context.Context, exe string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "version", "--json")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var v struct {
		Data struct{ Version string } `json:"data"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", err
	}
	if v.Data.Version == "" {
		return "", errors.New("no version reported")
	}
	return v.Data.Version, nil
}
