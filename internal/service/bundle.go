package service

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/driga/internal/self"
)

// BundlePath identifies our CLI's app layout, including when invoked via a
// Homebrew symlink. Other apps and ordinary development binaries are excluded.
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
	if filepath.Base(contents) != "Contents" || filepath.Base(app) != "Driga.app" {
		return ""
	}
	return app
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

// IsDaemon reports whether an executable name is drigad's.
func IsDaemon(binPath string) bool {
	name := strings.TrimSuffix(filepath.Base(binPath), filepath.Ext(binPath))
	return name == "drigad" || strings.HasPrefix(name, "drigad-")
}

// PlistStale is a read-only diagnostic. Compare path strings, not inode identity:
// an old bare PATH symlink can still resolve to the new bundle executable.
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
	return executable != DaemonPath(ExecutablePath(binPath)) || (BundlePath(binPath) != "" && !associated), nil
}

// plistEnv is the value the collector plist at path sets for the environment
// variable key, or "" when it sets none.
func plistEnv(path, key string) (string, error) {
	data, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer data.Close()
	decoder := xml.NewDecoder(data)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "key" {
			continue
		}
		var name string
		if err := decoder.DecodeElement(&name, &start); err != nil {
			return "", err
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
				return "", err
			}
			if start, ok = token.(xml.StartElement); ok {
				break
			}
		}
		if start.Name.Local != "dict" {
			return "", errors.New("collector plist expects a dict for EnvironmentVariables")
		}
		if err := decoder.DecodeElement(&dict, &start); err != nil {
			return "", err
		}
		for i := 0; i+1 < len(dict.Entries); i += 2 {
			if dict.Entries[i].XMLName.Local == "key" && dict.Entries[i].Text == key {
				return dict.Entries[i+1].Text, nil
			}
		}
		return "", nil
	}
}
