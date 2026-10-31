package scripts_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
    app="${@: -1}"
    if [ "$1" = --force ] && [ "${app%.app}" != "$app" ]; then
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
				helperSign := strings.Index(calls, "--options runtime --timestamp "+filepath.Join(root, "target", "Driga.app", "Contents", "Helpers", "Driga Service.app")+"\n")
				outerSign := strings.Index(calls, "--options runtime --timestamp "+filepath.Join(root, "target", "Driga.app")+"\n")
				if helperSign < 0 || outerSign <= helperSign {
					t.Fatal("helper must be signed with hardened runtime before outer app:", calls)
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

// Finder, Dock, Spotlight and login-item launches reach app mode only through
// LSEnvironment, and the app switches its own activation policy, so the plist
// must not carry LSUIElement.
func TestMacOSAppPlistLaunchesInAppMode(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	app := filepath.Join(t.TempDir(), "Driga.app")
	dir := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(dir, "driga"), "fixture", 0o700)
	writeFixtureFile(t, filepath.Join(dir, "drigad"), "fixture", 0o700)
	command := exec.CommandContext(t.Context(), "python3", "macos-app.py", app, "2026.10.5-alpha")
	command.Env = releaseTestEnv(t)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("assemble app: %v: %s", err, out)
	}
	dump := exec.CommandContext(t.Context(), "python3", "-c", "import json,plistlib,sys; print(json.dumps(plistlib.load(open(sys.argv[1],'rb'))))", filepath.Join(app, "Contents", "Info.plist"))
	dump.Env = releaseTestEnv(t)
	out, err := dump.Output()
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatal(err)
	}
	if _, present := info["LSUIElement"]; present {
		t.Error("Info.plist still sets LSUIElement")
	}
	env, _ := info["LSEnvironment"].(map[string]any)
	if len(env) != 1 || env["DRIGA_APP"] != "1" {
		t.Errorf("LSEnvironment = %v, want only DRIGA_APP=1", info["LSEnvironment"])
	}
	if info["NSHighResolutionCapable"] != true {
		t.Errorf("NSHighResolutionCapable = %v, want true", info["NSHighResolutionCapable"])
	}
}

func TestMacOSHelperPlist(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	app := filepath.Join(t.TempDir(), "Driga.app")
	bin := filepath.Join(app, "Contents", "MacOS", "driga")
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, bin, "fixture", 0700)
	cmd := exec.CommandContext(t.Context(), "python3", "macos-app.py", app, "2026.10.23-alpha")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	check := `import pathlib,plistlib,sys
app=pathlib.Path(sys.argv[1])/"Contents"
h=app/"Helpers/Driga Service.app/Contents"
i=plistlib.loads((h/"Info.plist").read_bytes())
assert i["CFBundleIdentifier"] == "run.driga.service"
assert i["CFBundleExecutable"] == "drigad"
assert i["CFBundlePackageType"] == "APPL"
assert i["LSBackgroundOnly"] is True
assert "LSEnvironment" not in i
assert i["CFBundleVersion"] == "2026.10.23"
assert i["DrigaReleaseVersion"] == "2026.10.23-alpha"
assert (h/"Resources"/i["CFBundleIconFile"]).read_bytes() == (app/"Resources/Driga.icns").read_bytes()
assert (h/"MacOS/drigad").read_bytes() == (app/"MacOS/driga").read_bytes()
assert sys.platform == "win32" or (h/"MacOS/drigad").stat().st_mode & 0o111  # Windows has no execute bits
assert not (app/"MacOS/drigad").exists()
`
	cmd = exec.CommandContext(t.Context(), "python3", "-c", check, app)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestMacOSReleaseVerificationOrder(t *testing.T) {
	data, err := os.ReadFile("verify-release-dist.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	helper := strings.Index(s, `codesign --verify --strict --verbose=2 "$app_temp/Driga.app/Contents/Helpers/Driga Service.app"`)
	outer := strings.Index(s, `codesign --verify --deep --strict --verbose=2 "$app_temp/Driga.app"`)
	if helper < 0 || outer <= helper {
		t.Fatal("verify nested bundle before outer seal")
	}
}

// Execute the generated Ruby block with a fake cask DSL. No installation or
// launchctl is reachable; only an existing service should trigger migration.
func TestMacOSCaskRepairsExistingServiceOnly(t *testing.T) {
	if _, err := exec.LookPath("ruby"); err != nil {
		t.Skip("ruby unavailable")
	}
	var config struct {
		Casks []struct {
			Block string `yaml:"custom_block"`
		} `yaml:"homebrew_casks"`
	}
	if err := yaml.Unmarshal([]byte(readRepositoryFile(t, "../.goreleaser.yaml")), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Casks) != 1 {
		t.Fatal("expected one cask")
	}
	for _, existing := range []bool{false, true} {
		setup := `
$calls = []
def app(name); end
def appdir; "/Applications"; end
def postflight; yield; end
def system_command(name, **opts)
 $calls << [name, opts]
 Struct.new(:stdout).new('{"EnvironmentVariables":{"DRIGA_DATA_DIR":"/Users/me/Library/Application Support/driga","PATH":"/custom/bin:/usr/bin"}}')
end
class File
 def self.exist?(path)
  raise "wrong migration guard" unless path.end_with?("/Library/LaunchAgents/run.driga.service.plist")
  ENV["EXISTING_SERVICE"] == "true"
 end
end
`
		check := `
if ENV["EXISTING_SERVICE"] == "true"
 raise "plist not read" unless $calls.length == 2 && $calls[0] == ["/usr/bin/plutil", {args: ["-convert", "json", "-o", "-", File.expand_path("~/Library/LaunchAgents/run.driga.service.plist")], must_succeed: true}]
 # env: would reach the service as "Application\\ Support"; /usr/bin/env args do not.
 expected = ["/usr/bin/env", {args: ["DRIGA_DATA_DIR=/Users/me/Library/Application Support/driga", "PATH=/custom/bin:/usr/bin", "/Applications/Driga.app/Contents/MacOS/driga", "service", "install"], must_succeed: true}]
 raise "environment or command changed: #{$calls}" unless $calls[1] == expected
else
 raise "fresh install started service" unless $calls.empty?
end
`
		cmd := exec.CommandContext(t.Context(), "ruby", "-e", setup+config.Casks[0].Block+check)
		cmd.Env = append(releaseTestEnv(t), "EXISTING_SERVICE="+fmt.Sprint(existing))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("existing=%v: %v: %s", existing, err, out)
		}
	}
}
