// Command windows-resources generates the Windows-only COFF resource objects.
// It runs on the build host; no Windows SDK, resource compiler, or cgo is used.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const winresTool = "github.com/tc-hib/go-winres@v0.3.3"

var calver = regexp.MustCompile(`^([1-9][0-9]{3})\.([1-9]|1[0-2])\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

func main() {
	root := flag.String("root", ".", "repository root")
	version := flag.String("version", "dev", "full release or snapshot version, without v")
	flag.Parse()
	// The CLI and the Windows app carry the same icon and version resources.
	for dir, file := range map[string]string{"driga": "driga.exe", "driga-app": "Driga.exe"} {
		if err := generate(*root, filepath.Join(*root, "cmd", dir), *version, file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

// Windows stores each numeric component in a WORD. Reject overflow rather than
// silently wrapping a release revision. Suffixes stay in the string table.
func numericVersion(version string) (string, error) {
	if version == "dev" {
		return "0.0.0.0", nil
	}
	parts := calver.FindStringSubmatch(version)
	if parts == nil {
		return "", fmt.Errorf("invalid Windows release version %q: want YYYY.M.REVISION[-suffix] or dev", version)
	}
	for _, part := range parts[1:4] {
		if _, err := strconv.ParseUint(part, 10, 16); err != nil {
			return "", fmt.Errorf("Windows version component %q exceeds 65535", part)
		}
	}
	return strings.Join(parts[1:4], ".") + ".0", nil
}

func versionStrings(version, file string) map[string]string {
	return map[string]string{
		"ProductName": "Driga", "FileDescription": "Driga",
		"CompanyName": "LabStack LLC", "LegalCopyright": "Copyright 2026 LabStack LLC",
		"OriginalFilename": file, "InternalName": strings.TrimSuffix(strings.ToLower(file), ".exe"),
		"FileVersion": version, "ProductVersion": version,
	}
}

func generate(root, output, version, file string) error {
	numeric, err := numericVersion(version)
	if err != nil {
		return err
	}
	icon, err := os.ReadFile(filepath.Join(root, "internal", "web", "static", "brand", "murmur.ico"))
	if err != nil {
		return err
	}
	fixed := map[string]string{"file_version": numeric, "product_version": numeric}
	if strings.Contains(version, "-") || version == "dev" {
		fixed["flags"] = "Prerelease"
	}
	resources := map[string]any{
		"RT_GROUP_ICON": map[string]any{"#1": map[string]any{"0000": "murmur.ico"}},
		"RT_VERSION": map[string]any{"#1": map[string]any{"0000": map[string]any{
			"fixed": fixed, "info": map[string]any{"0409": versionStrings(version, file)},
		}}},
	}
	body, err := json.Marshal(resources)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "driga-winres-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "murmur.ico"), icon, 0o600); err != nil {
		return err
	}
	input := filepath.Join(tmp, "winres.json")
	if err := os.WriteFile(input, body, 0o600); err != nil {
		return err
	}
	// Generate both objects together before GoReleaser starts parallel builds.
	// Do not use git-tag: GoReleaser's version also covers synthetic snapshots.
	cmd := exec.Command("go", "run", winresTool, "make", "--arch", "amd64,arm64", "--in", input, "--out", filepath.Join(output, "rsrc"))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
