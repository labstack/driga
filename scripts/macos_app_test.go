package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacOSAppBuildHook(t *testing.T) {
	requireArtifactVerifierTools(t)
	for _, test := range []struct {
		name, snapshot, status string
		succeeds               bool
	}{
		{"unsigned snapshot", "true", "", true},
		{"accepted notarization", "false", "Accepted", true},
		{"rejected notarization", "false", "Invalid", false},
		{"missing keychain", "false", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			binDir := filepath.Join(root, "bin")
			executable := filepath.Join(root, "target", "Driga.app", "Contents", "MacOS", "driga")
			for _, dir := range []string{binDir, filepath.Dir(executable)} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeFixtureFile(t, executable, "synthetic executable", 0o700)
			// Every signing/Apple tool is fake; no real signing identity,
			// keychain, notarization service, launchd or user data is reachable.
			fake := `#!/bin/bash
set -eu
name=$(basename "$0")
printf '%s %s\n' "$name" "$*" >> "$FAKE_TOOL_LOG"
case "$name" in
  uname) printf 'Darwin\n' ;;
  ditto) touch "${@: -1}" ;;
  codesign)
    if [ "$1" = --force ]; then
      app="${@: -1}"
      mkdir -p "$app/Contents/_CodeSignature"
      printf 'seal' > "$app/Contents/_CodeSignature/CodeResources"
    fi ;;
  xcrun)
    case "$1 $2" in
      'notarytool submit') printf '{"id":"fixture","status":"%s"}\n' "$FAKE_NOTARY_STATUS" ;;
      'notarytool log') printf '{"issues":[]}' > "${@: -1}" ;;
      'stapler staple') printf 'ticket' > "${@: -1}/Contents/CodeResources" ;;
    esac ;;
esac
`
			for _, name := range []string{"uname", "ditto", "codesign", "xcrun", "spctl"} {
				writeFixtureFile(t, filepath.Join(binDir, name), fake, 0o700)
			}
			repo, err := filepath.Abs("..")
			if err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), "bash", "scripts/build-macos-app.sh", executable, "darwin", "2026.10.0-alpha", test.snapshot)
			command.Dir = repo
			log := filepath.Join(root, "tools.log")
			command.Env = append(releaseTestEnv(t), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_TOOL_LOG="+log, "FAKE_NOTARY_STATUS="+test.status, "DRIGA_SIGNING_KEYCHAIN=", "DRIGA_SIGNING_IDENTITY=", "DRIGA_NOTARY_KEY=", "DRIGA_NOTARY_KEY_ID=", "DRIGA_NOTARY_ISSUER=")
			if test.status != "" {
				command.Env = append(command.Env, "DRIGA_SIGNING_KEYCHAIN="+filepath.Join(root, "fake.keychain"), "DRIGA_SIGNING_IDENTITY=synthetic identity", "DRIGA_NOTARY_KEY="+filepath.Join(root, "notary.p8"), "DRIGA_NOTARY_KEY_ID=KEYID", "DRIGA_NOTARY_ISSUER=issuer")
			}
			output, err := command.CombinedOutput()
			if (err == nil) != test.succeeds {
				t.Fatalf("hook: %v: %s", err, output)
			}
			trace, readErr := os.ReadFile(log)
			if test.snapshot == "true" {
				if !os.IsNotExist(readErr) {
					t.Fatalf("snapshot invoked signing tools: %s", trace)
				}
			} else if test.status != "" {
				calls := string(trace)
				if !strings.Contains(calls, "--options runtime --timestamp") || !strings.Contains(calls, "notarytool submit") || !strings.Contains(calls, "--key-id KEYID") || strings.Contains(calls, "keychain-profile") {
					t.Fatal(calls)
				}
				staple := strings.Contains(calls, "stapler staple")
				if staple != test.succeeds {
					t.Fatalf("staple on %s: %s", test.status, calls)
				}
				if test.succeeds && strings.Index(calls, "notarytool submit") > strings.Index(calls, "stapler staple") {
					t.Fatal("stapled before notarization")
				}
			}
			resources := filepath.Join(root, "target", "bundle-resources", "Contents")
			_, stageErr := os.Stat(filepath.Join(resources, "Info.plist"))
			if (stageErr == nil) != test.succeeds {
				t.Fatalf("archive staging: %v", stageErr)
			}
			if test.succeeds {
				if _, err := os.Stat(filepath.Join(resources, "MacOS", "driga")); !os.IsNotExist(err) {
					t.Fatal("staged executable duplicates the build artifact")
				}
				if test.status == "Accepted" {
					if _, err := os.Stat(filepath.Join(resources, "CodeResources")); err != nil {
						t.Fatal("ticket wasn't staged", err)
					}
				}
			}
		})
	}
}

func TestMacOSAppMetadataRejectsInvalidVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix release tooling")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	app := filepath.Join(t.TempDir(), "Driga.app")
	dir := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(dir, "driga"), "fixture", 0o700)
	command := exec.CommandContext(t.Context(), "python3", "macos-app.py", app, "invalid&version")
	command.Env = releaseTestEnv(t)
	if out, err := command.CombinedOutput(); err == nil {
		t.Fatalf("invalid version accepted: %s", out)
	}
}
