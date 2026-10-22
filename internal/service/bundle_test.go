package service

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLaunchdPlistBundleAndDevelopment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bundled := filepath.Join(root, "Driga.app", "Contents", "MacOS", "driga")
	if err := os.MkdirAll(filepath.Dir(bundled), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "driga")
	if err := os.Symlink(bundled, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, executable string
		bundle                 bool
	}{
		{"development", "/opt/dev & tools/driga", "/opt/dev & tools/driga", false},
		{"bundle", bundled, bundled, true},
		{"Homebrew symlink", link, bundled, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := configForOS(tc.path, "darwin")
			body, err := renderLaunchd(c)
			if err != nil {
				t.Fatal(err)
			}
			var plist struct {
				Dict struct {
					Keys   []string `xml:"key"`
					Arrays []struct {
						Strings []string `xml:"string"`
					} `xml:"array"`
				} `xml:"dict"`
			}
			if err := xml.Unmarshal(body, &plist); err != nil {
				t.Fatal(err)
			}
			if c.Executable != tc.executable {
				t.Fatalf("executable = %s", c.Executable)
			}
			arrays := plist.Dict.Arrays
			args := arrays[len(arrays)-1].Strings
			if len(args) != 2 || args[0] != tc.executable || args[1] != "collect" {
				t.Fatalf("ProgramArguments: %v", args)
			}
			associated := strings.Contains(string(body), "AssociatedBundleIdentifiers")
			if associated != tc.bundle {
				t.Fatalf("association: %s", body)
			}
			if tc.bundle && (len(arrays[0].Strings) != 1 || arrays[0].Strings[0] != "run.driga") {
				t.Fatalf("association: %s", body)
			}
			plistPath := filepath.Join(t.TempDir(), "collector.plist")
			if err := os.WriteFile(plistPath, body, 0o600); err != nil {
				t.Fatal(err)
			}
			stale, err := PlistStale(plistPath, tc.path)
			if err != nil || stale {
				t.Fatalf("fresh plist = %t, %v", stale, err)
			}
			old, err := renderLaunchd(configForOS("/opt/homebrew/bin/driga", "darwin"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(plistPath, old, 0o600); err != nil {
				t.Fatal(err)
			}
			stale, err = PlistStale(plistPath, tc.path)
			if err != nil || !stale {
				t.Fatalf("stale plist = %t, %v", stale, err)
			}
		})
	}
	path := filepath.Join(root, "missing.plist")
	if stale, err := PlistStale(path, bundled); stale || err != nil {
		t.Fatalf("missing: %t %v", stale, err)
	}
	if err := os.WriteFile(path, []byte("<plist><dict>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlistStale(path, bundled); err == nil {
		t.Fatal("malformed plist accepted")
	}
}
