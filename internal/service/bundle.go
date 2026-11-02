package service

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/self"
)

// BundleID is Driga.app's bundle identifier. The app is found by it and by
// its layout, never by its name, so a renamed Driga.app still works.
const BundleID = "run.driga"

// BundlePath identifies our CLI's app layout, including when invoked via a
// Homebrew symlink: driga or drigad in <name>.app/Contents/MacOS, or the
// nested Driga Service.app's drigad, in a bundle whose Info.plist names
// run.driga. Other apps and ordinary development binaries are excluded.
func BundlePath(binPath string) string {
	if resolved, err := filepath.EvalSymlinks(binPath); err == nil {
		binPath = resolved
	}
	binPath = self.CLI(binPath)
	if base := filepath.Base(binPath); base != "driga" && base != "drigad" || filepath.Base(filepath.Dir(binPath)) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(filepath.Dir(binPath))
	app := filepath.Dir(contents)
	if filepath.Base(contents) != "Contents" || filepath.Ext(app) != ".app" || bundleIdentifier(app) != BundleID {
		return ""
	}
	return app
}

// bundleIdentifier is the CFBundleIdentifier of the app at path, or "".
func bundleIdentifier(app string) string {
	f, err := os.Open(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	defer f.Close()
	decoder := xml.NewDecoder(f)
	depth, key := 0, ""
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		switch t := token.(type) {
		case xml.EndElement:
			depth--
		case xml.StartElement:
			depth++
			// plist > dict > its keys and values
			if depth != 3 || t.Name.Local != "key" && t.Name.Local != "string" {
				continue
			}
			var text string
			if err := decoder.DecodeElement(&text, &t); err != nil {
				return ""
			}
			depth--
			if t.Name.Local == "key" {
				key = text
			} else if key == "CFBundleIdentifier" {
				return text
			}
		}
	}
}

// ExecutablePath keeps ordinary builds' stable PATH entry, but launchd needs
// the actual bundle executable for macOS background-item attribution.
func ExecutablePath(binPath string) string {
	if app := BundlePath(binPath); app != "" {
		return filepath.Join(app, "Contents", "MacOS", "driga")
	}
	return binPath
}

// DaemonPath selects the nested helper for Driga.app. Outside the bundle,
// drigad sits beside driga (including development variants and Windows).
// Started under that name, the same program runs the Driga service.
func DaemonPath(binPath string) string {
	if app := BundlePath(binPath); app != "" {
		return filepath.Join(app, "Contents", "Helpers", "Driga Service.app", "Contents", "MacOS", "drigad")
	}
	dir, base := filepath.Split(binPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if rest, ok := strings.CutPrefix(name, "driga"); ok && !strings.HasPrefix(rest, "d") {
		return filepath.Join(dir, "drigad"+rest+ext)
	}
	return binPath
}

// CheckDaemon reports a drigad the service for the CLI at binPath would run
// that is missing or not executable: a source build of driga alone has none.
func CheckDaemon(binPath string) error {
	daemon := DaemonPath(ExecutablePath(binPath))
	info, err := os.Stat(daemon)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the Driga service executable %s does not exist; build drigad beside driga (just build makes both, or go build -o %s ./cmd/driga)", daemon, daemon)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("the Driga service executable %s is not an executable file", daemon)
	}
	return nil
}

// IsDaemon reports whether an executable name is drigad's.
func IsDaemon(binPath string) bool {
	name := strings.TrimSuffix(filepath.Base(binPath), filepath.Ext(binPath))
	return name == "drigad" || strings.HasPrefix(name, "drigad-")
}

// PlistStale is a read-only diagnostic: the plist runs another executable, lacks
// the bundle association, or collects into another data directory than this
// process uses, so its clients would find no service. Compare path strings,
// not inode identity: an old bare PATH symlink can still resolve to the new
// bundle executable.
func PlistStale(path, binPath string) (bool, error) {
	data, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer data.Close()
	decoder := xml.NewDecoder(data)
	executable, associated := "", false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "key" {
			continue
		}
		var key string
		if err := decoder.DecodeElement(&key, &start); err != nil {
			return false, err
		}
		if key != "ProgramArguments" && key != "AssociatedBundleIdentifiers" {
			continue
		}
		for {
			token, err := decoder.Token()
			if err != nil {
				return false, err
			}
			start, ok = token.(xml.StartElement)
			if !ok {
				continue
			}
			if start.Name.Local != "array" {
				return false, errors.New("service plist expects an array for " + key)
			}
			var array struct {
				Strings []string `xml:"string"`
			}
			if err := decoder.DecodeElement(&array, &start); err != nil {
				return false, err
			}
			if key == "ProgramArguments" && len(array.Strings) > 0 {
				executable = array.Strings[0]
			}
			if key == "AssociatedBundleIdentifiers" {
				for _, id := range array.Strings {
					if id == "run.driga.service" {
						associated = true
					}
				}
			}
			break
		}
	}
	if executable == "" {
		return false, errors.New("service plist has no ProgramArguments executable")
	}
	if executable != DaemonPath(ExecutablePath(binPath)) || (BundlePath(binPath) != "" && !associated) {
		return true, nil
	}
	dir, err := plistEnv(path, "DRIGA_DATA_DIR")
	if err != nil {
		return false, err
	}
	return resolveDataDir(dir) != config.DataDir(), nil
}

// plistEnv is the value the collector plist at path sets for the environment
// variable key, or "" when it sets none.
func plistEnv(path, key string) (string, error) {
	env, err := plistEnvironment(path)
	return env[key], err
}

// plistEnvironment is the EnvironmentVariables dictionary of the collector
// plist at path; empty when it sets none.
func plistEnvironment(path string) (map[string]string, error) {
	data, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer data.Close()
	decoder := xml.NewDecoder(data)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return map[string]string{}, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "key" {
			continue
		}
		var name string
		if err := decoder.DecodeElement(&name, &start); err != nil {
			return nil, err
		}
		if name != "EnvironmentVariables" {
			continue
		}
		// The dict that follows alternates keys and string values.
		var dict struct {
			Entries []struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			} `xml:",any"`
		}
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if start, ok = token.(xml.StartElement); ok {
				break
			}
		}
		if start.Name.Local != "dict" {
			return nil, errors.New("collector plist expects a dict for EnvironmentVariables")
		}
		if err := decoder.DecodeElement(&dict, &start); err != nil {
			return nil, err
		}
		env := map[string]string{}
		for i := 0; i+1 < len(dict.Entries); i += 2 {
			if dict.Entries[i].XMLName.Local == "key" {
				env[dict.Entries[i].Text] = dict.Entries[i+1].Text
			}
		}
		return env, nil
	}
}
