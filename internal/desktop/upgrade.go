package desktop

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	mu    sync.Mutex
	stamp time.Time
}

func newUpgradeWatch(exe, version string) *upgradeWatch {
	return &upgradeWatch{exe: exe, version: version, stat: modTime, read: installedVersion}
}

// errUpgradePending holds collector restarts while the executable changed.
var errUpgradePending = errors.New("waiting for Driga to restart after an update")

// start records the executable as it is now.
func (w *upgradeWatch) start() {
	stamp, _ := w.stat(w.exe)
	w.mu.Lock()
	w.stamp = stamp
	w.mu.Unlock()
}

// pending reports whether the executable changed since the watch last read
// its version. A collector started now could be another version than the
// app, and migrate the store the app reads.
func (w *upgradeWatch) pending() bool {
	stamp, err := w.stat(w.exe)
	w.mu.Lock()
	defer w.mu.Unlock()
	return err == nil && !w.stamp.IsZero() && !stamp.Equal(w.stamp)
}

// changed reports whether the executable now holds another version. It asks
// the executable only after its modification time moved, and asks again on
// the next check while it cannot answer, as during an upgrade.
func (w *upgradeWatch) changed(ctx context.Context) bool {
	stamp, err := w.stat(w.exe)
	w.mu.Lock()
	unchanged := stamp.Equal(w.stamp)
	w.mu.Unlock()
	if err != nil || unchanged {
		return false
	}
	version, err := w.read(ctx, w.exe)
	if err != nil {
		return false
	}
	if version != w.version {
		return true
	}
	w.mu.Lock()
	w.stamp = stamp
	w.mu.Unlock()
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

// installedVersion reads the version of the app bundle the executable is
// in from its Info.plist, and asks any other executable. Running a freshly
// installed bundle can wait on Gatekeeper's first-launch prompt.
func installedVersion(ctx context.Context, exe string) (string, error) {
	contents := filepath.Dir(filepath.Dir(exe))
	if filepath.Base(filepath.Dir(exe)) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(filepath.Dir(contents), ".app") {
		f, err := os.Open(filepath.Join(contents, "Info.plist"))
		if err != nil {
			return "", err
		}
		defer f.Close()
		return plistString(f, "DrigaReleaseVersion")
	}
	return exeVersion(ctx, exe)
}

// plistString returns the string under key in an XML property list's
// top-level dictionary.
func plistString(r io.Reader, key string) (string, error) {
	dec := xml.NewDecoder(r)
	depth, found := 0, false
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", errors.New(key + " not in Info.plist")
			}
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			// plist > dict > key/string: the top-level entries sit at depth 3.
			if depth != 3 {
				continue
			}
			var text string
			if err := dec.DecodeElement(&text, &t); err != nil {
				return "", err
			}
			depth--
			if found {
				if t.Name.Local != "string" || text == "" {
					return "", errors.New(key + " is not a string")
				}
				return text, nil
			}
			found = t.Name.Local == "key" && text == key
		case xml.EndElement:
			depth--
		}
	}
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
