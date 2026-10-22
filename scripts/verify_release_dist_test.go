package scripts_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseDistVerifierResolvesGeneratedCaskVersion(t *testing.T) {
	requireArtifactVerifierTools(t)

	t.Run("accepts GoReleaser interpolation", func(t *testing.T) {
		dist, binDir := releaseDistFixture(t, "2026.10.0-alpha", "v2026.10.0-alpha")
		output, err := runArtifactVerifier(t, dist, binDir)
		if err != nil {
			t.Fatalf("verifier rejected a generated Cask: %v\n%s", err, output)
		}
	})

	t.Run("binds interpolation to declared version", func(t *testing.T) {
		dist, binDir := releaseDistFixture(t, "2026.8.1", "v2026.8.1")
		cask := filepath.Join(dist, "homebrew", "Casks", "driga.rb")
		replaceFixtureText(t, cask, `version "2026.8.1"`, `version "2026.8.0"`)

		output, err := runArtifactVerifier(t, dist, binDir)
		if err == nil {
			t.Fatalf("verifier accepted a stale Cask version:\n%s", output)
		}
		if !strings.Contains(output, "Homebrew Cask version does not match 2026.8.1") {
			t.Fatalf("verifier did not explain the Cask version mismatch:\n%s", output)
		}
	})

	t.Run("compares the complete resolved URL", func(t *testing.T) {
		dist, binDir := releaseDistFixture(t, "2026.8.1", "v2026.8.1")
		cask := filepath.Join(dist, "homebrew", "Casks", "driga.rb")
		replaceFixtureText(t, cask, "github.com/labstack/driga", "github.com/example/driga")

		output, err := runArtifactVerifier(t, dist, binDir)
		if err == nil {
			t.Fatalf("verifier accepted a Cask from the wrong repository:\n%s", output)
		}
		if !strings.Contains(output, "Homebrew Cask amd64 URL =") {
			t.Fatalf("verifier did not explain the Cask URL mismatch:\n%s", output)
		}
	})
}

func TestReleaseDistVerifierRejectsInvalidWindowsResources(t *testing.T) {
	requireArtifactVerifierTools(t)
	dist, binDir := releaseDistFixture(t, "2026.10.5-alpha", "v2026.10.5-alpha")
	writeFixtureFile(t, filepath.Join(binDir, "go"), "#!/bin/sh\necho 'invalid Windows PE resources' >&2\nexit 1\n", 0o700)
	output, err := runArtifactVerifier(t, dist, binDir)
	if err == nil || !strings.Contains(output, "invalid Windows PE resources") {
		t.Fatalf("verifier did not propagate resource inspection failure: %v\n%s", err, output)
	}
}

func requireArtifactVerifierTools(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("release artifact verification requires Unix archive tools")
	}
	for _, tool := range []string{"bash", "jq", "tar", "unzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is unavailable: %v", tool, err)
		}
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		if _, shasumErr := exec.LookPath("shasum"); shasumErr != nil {
			t.Skip("neither sha256sum nor shasum is available")
		}
	}
}

func releaseDistFixture(t *testing.T, version, tag string) (string, string) {
	t.Helper()
	dist := filepath.Join(t.TempDir(), "dist")
	for _, dir := range []string{
		filepath.Join(dist, "homebrew", "Casks"),
		filepath.Join(dist, "scoop"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFixtureFile(t, filepath.Join(dist, "metadata.json"), fmt.Sprintf("{\"version\":%q,\"tag\":%q}\n", version, tag), 0o600)

	artifacts := []string{
		fmt.Sprintf("driga_%s_darwin_amd64.tar.gz", version),
		fmt.Sprintf("driga_%s_darwin_arm64.tar.gz", version),
		fmt.Sprintf("driga_%s_linux_amd64.deb", version),
		fmt.Sprintf("driga_%s_linux_amd64.rpm", version),
		fmt.Sprintf("driga_%s_linux_amd64.tar.gz", version),
		fmt.Sprintf("driga_%s_linux_arm64.deb", version),
		fmt.Sprintf("driga_%s_linux_arm64.rpm", version),
		fmt.Sprintf("driga_%s_linux_arm64.tar.gz", version),
		fmt.Sprintf("driga_%s_windows_amd64.zip", version),
		fmt.Sprintf("driga_%s_windows_arm64.zip", version),
	}
	for _, platform := range []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"} {
		writeTarGzipFixture(t, filepath.Join(dist, fmt.Sprintf("driga_%s_%s.tar.gz", version, platform)))
	}
	for _, arch := range []string{"amd64", "arm64"} {
		writeZipFixture(t, filepath.Join(dist, fmt.Sprintf("driga_%s_windows_%s.zip", version, arch)))
		writeFixtureFile(t, filepath.Join(dist, fmt.Sprintf("driga_%s_linux_%s.deb", version, arch)), "deb fixture\n", 0o600)
		writeFixtureFile(t, filepath.Join(dist, fmt.Sprintf("driga_%s_linux_%s.rpm", version, arch)), "rpm fixture\n", 0o600)
	}

	hashes := make(map[string]string, len(artifacts))
	var checksums strings.Builder
	for _, artifact := range artifacts {
		hashes[artifact] = fixtureSHA256(t, filepath.Join(dist, artifact))
		fmt.Fprintf(&checksums, "%s  %s\n", hashes[artifact], artifact)
	}
	writeFixtureFile(t, filepath.Join(dist, fmt.Sprintf("driga_%s_checksums.txt", version)), checksums.String(), 0o600)

	scoop := fmt.Sprintf(`{
  "version": %q,
  "architecture": {
    "64bit": {"url": "https://github.com/labstack/driga/releases/download/%s/driga_%s_windows_amd64.zip", "bin": ["driga.exe"], "hash": %q},
    "arm64": {"url": "https://github.com/labstack/driga/releases/download/%s/driga_%s_windows_arm64.zip", "bin": ["driga.exe"], "hash": %q}
  }
}
`, version, tag, version, hashes[fmt.Sprintf("driga_%s_windows_amd64.zip", version)], tag, version, hashes[fmt.Sprintf("driga_%s_windows_arm64.zip", version)])
	writeFixtureFile(t, filepath.Join(dist, "scoop", "driga.json"), scoop, 0o600)

	cask := fmt.Sprintf(`cask "driga" do
  version %q

  on_macos do
    on_intel do
      sha256 %q
      url "https://github.com/labstack/driga/releases/download/v#{version}/driga_#{version}_darwin_amd64.tar.gz"
    end
    on_arm do
      sha256 %q
      url "https://github.com/labstack/driga/releases/download/v#{version}/driga_#{version}_darwin_arm64.tar.gz"
    end
  end

  binary "driga"
end
`, version, hashes[fmt.Sprintf("driga_%s_darwin_amd64.tar.gz", version)], hashes[fmt.Sprintf("driga_%s_darwin_arm64.tar.gz", version)])
	writeFixtureFile(t, filepath.Join(dist, "homebrew", "Casks", "driga.rb"), cask, 0o600)

	nativeDir := nativeBinaryFixtureDir(t, dist)
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(nativeDir, "driga"), fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '{\"schema\":2,\"data\":{\"version\":\"%s\",\"commit\":\"0123456789abcdef0123456789abcdef01234567\",\"date\":\"2026-08-16T00:00:00Z\"}}'\n", version), 0o700)

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(binDir, "dpkg-deb"), "#!/bin/sh\nprintf '%s\\n' ' ./usr/bin/driga'\n", 0o700)
	// These packaging fixtures deliberately contain fake executables. The real
	// PE parser and generated resources are exercised in windows-resources.
	writeFixtureFile(t, filepath.Join(binDir, "go"), "#!/bin/sh\n[ \"$1\" = test ] && [ \"$2\" = ./scripts/windows-resources ]\n", 0o700)
	return dist, binDir
}

func nativeBinaryFixtureDir(t *testing.T, dist string) string {
	t.Helper()
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return filepath.Join(dist, "driga_darwin_arm64_v8.0")
	case "darwin/amd64":
		return filepath.Join(dist, "driga_darwin_amd64_v1")
	case "linux/arm64":
		return filepath.Join(dist, "driga_linux_arm64_v8.0")
	case "linux/amd64":
		return filepath.Join(dist, "driga_linux_amd64_v1")
	default:
		t.Skipf("unsupported artifact verifier host %s/%s", runtime.GOOS, runtime.GOARCH)
		return ""
	}
}

func runArtifactVerifier(t *testing.T, dist, binDir string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", "verify-release-dist.sh", dist)
	cmd.Env = append(releaseTestEnv(t), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeTarGzipFixture(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range []struct {
		name string
		body string
		mode int64
	}{{"LICENSE", "license\n", 0o644}, {"NOTICE", "notice\n", 0o644}, {"README.md", "fixture\n", 0o644}, {"driga", "binary\n", 0o755}, {"third-party/fonts-LICENSE.txt", "fonts\n", 0o644}, {"third-party/vendor-LICENSES.txt", "vendor\n", 0o644}} {
		body := []byte(entry.body)
		if err := tarWriter.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeZipFixture(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{"LICENSE", "NOTICE", "README.md", "driga.exe", "third-party/fonts-LICENSE.txt", "third-party/vendor-LICENSES.txt"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("fixture\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixtureSHA256(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

func writeFixtureFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func replaceFixtureText(t *testing.T, path, old, replacement string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.ReplaceAll(string(body), old, replacement)
	if updated == string(body) {
		t.Fatalf("fixture %s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseDistVerifierChecksNativeVersionAndMetadata(t *testing.T) {
	requireArtifactVerifierTools(t)
	for _, test := range []struct{ name, old, replacement, want string }{
		{"version includes v", `"version":"2026.10.0-alpha"`, `"version":"v2026.10.0-alpha"`, "does not report release tag"},
		{"wrong version", `"version":"2026.10.0-alpha"`, `"version":"2026.10.1-alpha"`, "does not report release tag"},
		{"missing commit", `"commit":"0123456789abcdef0123456789abcdef01234567"`, `"commit":"unknown"`, "no embedded commit or date"},
		{"missing date", `"date":"2026-08-16T00:00:00Z"`, `"date":"unknown"`, "no embedded commit or date"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dist, binDir := releaseDistFixture(t, "2026.10.0-alpha", "v2026.10.0-alpha")
			replaceFixtureText(t, filepath.Join(nativeBinaryFixtureDir(t, dist), "driga"), test.old, test.replacement)
			output, err := runArtifactVerifier(t, dist, binDir)
			if err == nil || !strings.Contains(output, test.want) {
				t.Fatalf("want %s, got %v: %s", test.want, err, output)
			}
		})
	}
}

func TestReleaseDistVerifierAcceptsSnapshotVersion(t *testing.T) {
	requireArtifactVerifierTools(t)
	dist, binDir := releaseDistFixture(t, "2026.10.0-alpha-SNAPSHOT-abcdef0", "v2026.10.0-alpha")
	// Snapshot URLs use the real tag and synthetic version literally.
	cask := filepath.Join(dist, "homebrew", "Casks", "driga.rb")
	replaceFixtureText(t, cask, "download/v#{version}/", "download/v2026.10.0-alpha/")
	output, err := runArtifactVerifier(t, dist, binDir)
	if err != nil {
		t.Fatalf("snapshot rejected: %v: %s", err, output)
	}
}

func TestReleaseDistVerifierRejectsMissingBundledNotices(t *testing.T) {
	requireArtifactVerifierTools(t)
	dist, binDir := releaseDistFixture(t, "2026.10.0-alpha", "v2026.10.0-alpha")
	archive := filepath.Join(dist, "driga_2026.10.0-alpha_windows_arm64.zip")
	before := fixtureSHA256(t, archive)
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{"LICENSE", "NOTICE", "README.md", "driga.exe", "third-party/fonts-LICENSE.txt"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("fixture\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	// Bind the checksum to the modified archive, so its missing notice is
	// the reason verification fails before package metadata is inspected.
	after := fixtureSHA256(t, archive)
	replaceFixtureText(t, filepath.Join(dist, "driga_2026.10.0-alpha_checksums.txt"), before+"  "+filepath.Base(archive), after+"  "+filepath.Base(archive))
	output, err := runArtifactVerifier(t, dist, binDir)
	if err == nil || !strings.Contains(output, "unexpected contents") {
		t.Fatalf("missing notice accepted: %v: %s", err, output)
	}
}
