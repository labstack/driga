package service

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// BundlePath identifies our CLI's app layout, including when invoked via a
// Homebrew symlink. Other apps and ordinary development binaries are excluded.
func BundlePath(binPath string) string {
	if resolved, err := filepath.EvalSymlinks(binPath); err == nil {
		binPath = resolved
	}
	if filepath.Base(binPath) != "driga" || filepath.Base(filepath.Dir(binPath)) != "MacOS" {
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
				return false, errors.New("collector plist expects an array for " + key)
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
					if id == "run.driga" {
						associated = true
					}
				}
			}
			break
		}
	}
	if executable == "" {
		return false, errors.New("collector plist has no ProgramArguments executable")
	}
	return executable != ExecutablePath(binPath) || (BundlePath(binPath) != "" && !associated), nil
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
