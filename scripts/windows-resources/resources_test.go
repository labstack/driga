package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

var releaseDist = flag.String("dist", "", "optional GoReleaser dist to inspect")
var releaseVersion = flag.String("version", "", "expected full release version")

func TestNumericVersion(t *testing.T) {
	for input, want := range map[string]string{
		"dev": "0.0.0.0", "2026.10.5-alpha": "2026.10.5.0",
		"2026.10.5-alpha-SNAPSHOT-abcdef0": "2026.10.5.0", "2026.1.65535": "2026.1.65535.0",
	} {
		got, err := numericVersion(input)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %s", input, got, err, want)
		}
	}
	for _, input := range []string{"", "v2026.10.5-alpha", "2026.13.1", "2026.01.1", "2026.1.65536", "2026.1.9999999999999999999"} {
		if _, err := numericVersion(input); err == nil {
			t.Errorf("accepted invalid version %q", input)
		}
	}
}

func TestCopyrightMatchesRepository(t *testing.T) {
	for _, path := range []string{"../../LICENSE", "../../NOTICE"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), versionStrings("dev")["LegalCopyright"]) {
			t.Errorf("VERSIONINFO copyright does not match %s", path)
		}
	}
}

// Build a tiny main with the real generated objects, without running any target
// executable. Comparing Unix builds before/after proves Go ignores these files.
func TestGeneratedResources(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module resource-test\n\ngo 1.27.3\n", "main.go": "package main\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	build := func(goos, arch string) []byte {
		t.Helper()
		out := filepath.Join(dir, "output", goos+"-"+arch)
		cmd := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", out, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED=0", "GOWORK=off")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s/%s: %v\n%s", goos, arch, err, output)
		}
		body, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	before := map[string][32]byte{}
	for _, goos := range []string{"darwin", "linux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			before[goos+arch] = sha256.Sum256(build(goos, arch))
		}
	}
	const version = "2026.10.5-alpha"
	if err := generate(root, dir, version); err != nil {
		t.Fatal(err)
	}
	objects := map[string][32]byte{}
	for _, arch := range []string{"amd64", "arm64"} {
		path := filepath.Join(dir, "rsrc_windows_"+arch+".syso")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		objects[path] = sha256.Sum256(body)
		verifyPE(t, build("windows", arch), arch, version)
	}
	if err := generate(root, dir, version); err != nil {
		t.Fatal(err)
	}
	for path, want := range objects {
		body, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(body) != want {
			t.Fatalf("resource generation is not deterministic: %s (%v)", path, err)
		}
	}
	for _, goos := range []string{"darwin", "linux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			if sha256.Sum256(build(goos, arch)) != before[goos+arch] {
				t.Errorf("Windows resources changed %s/%s binary", goos, arch)
			}
		}
	}
}

// The distribution verifier runs this against the archived shipping binaries,
// not intermediate objects. Normal go test still exercises generation above.
func TestReleaseResources(t *testing.T) {
	if *releaseDist == "" {
		t.Skip("no release dist supplied")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			archive := filepath.Join(*releaseDist, fmt.Sprintf("driga_%s_windows_%s.zip", *releaseVersion, arch))
			z, err := zip.OpenReader(archive)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			f, err := z.Open("driga.exe")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			body, err := io.ReadAll(f)
			if err != nil {
				t.Fatal(err)
			}
			verifyPE(t, body, arch, *releaseVersion)
		})
	}
}

func verifyPE(t *testing.T, body []byte, arch, version string) {
	t.Helper()
	f, err := pe.NewFile(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	wantMachine := map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64}[arch]
	if f.Machine != wantMachine {
		t.Fatalf("PE machine = %#x, want %#x", f.Machine, wantMachine)
	}
	header, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatal("want PE32+ optional header")
	}
	resourceRVA := header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress
	var data []byte
	var base uint32
	for _, section := range f.Sections {
		if resourceRVA >= section.VirtualAddress && resourceRVA-section.VirtualAddress < section.Size {
			data, err = section.Data()
			base = section.VirtualAddress
			break
		}
	}
	if err != nil || len(data) == 0 || resourceRVA == 0 {
		t.Fatalf("missing PE resources: %v", err)
	}
	// Walk IMAGE_RESOURCE_DIRECTORY: type -> ID -> language -> data entry.
	resources := map[uint32]map[uint32][]byte{}
	var walk func(uint32, []uint32)
	span := func(offset, size uint32) []byte {
		t.Helper()
		if uint64(offset)+uint64(size) > uint64(len(data)) {
			t.Fatal("resource extends past section")
		}
		return data[offset : offset+size]
	}
	walk = func(offset uint32, path []uint32) {
		header := span(offset, 16)
		count := uint32(binary.LittleEndian.Uint16(header[12:])) + uint32(binary.LittleEndian.Uint16(header[14:]))
		for i := uint32(0); i < count; i++ {
			entry := span(offset+16+i*8, 8)
			id, target := binary.LittleEndian.Uint32(entry), binary.LittleEndian.Uint32(entry[4:])
			if id&0x80000000 != 0 {
				t.Fatal("unexpected named resource")
			}
			next := append(slices.Clone(path), id)
			if target&0x80000000 != 0 {
				if len(next) >= 3 {
					t.Fatal("resource directory too deep")
				}
				walk(target&0x7fffffff, next)
				continue
			}
			if len(next) != 3 {
				t.Fatal("incomplete resource path")
			}
			entry = span(target, 16)
			rva, size := binary.LittleEndian.Uint32(entry), binary.LittleEndian.Uint32(entry[4:])
			if rva < base {
				t.Fatal("invalid resource RVA")
			}
			if resources[next[0]] == nil {
				resources[next[0]] = map[uint32][]byte{}
			}
			resources[next[0]][next[1]] = span(rva-base, size)
		}
	}
	// Directory offsets are relative to the start of the resource directory.
	if resourceRVA != base {
		t.Fatal("resource directory must start at section base")
	}
	walk(0, nil)
	versionInfo := resources[16][1] // RT_VERSION
	if len(versionInfo) < 92 {
		t.Fatal("missing or truncated VERSIONINFO")
	}
	utf16Bytes := func(s string) []byte {
		b := make([]byte, 2*(len(utf16.Encode([]rune(s)))+1))
		for i, v := range utf16.Encode([]rune(s)) {
			binary.LittleEndian.PutUint16(b[2*i:], v)
		}
		return b
	}
	// VS_VERSION_INFO's fixed value follows the DWORD-aligned, NUL-terminated key.
	fixedOffset := (6 + len(utf16Bytes("VS_VERSION_INFO")) + 3) &^ 3
	fixed := versionInfo[fixedOffset:]
	if binary.LittleEndian.Uint32(fixed) != 0xfeef04bd {
		t.Fatal("invalid VS_FIXEDFILEINFO signature")
	}
	numeric, err := numericVersion(version)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(numeric, ".")
	var words [4]uint32
	for i, p := range parts {
		n, _ := strconv.ParseUint(p, 10, 16)
		words[i] = uint32(n)
	}
	for _, offset := range []int{8, 16} { // file and product versions
		if binary.LittleEndian.Uint32(fixed[offset:]) != words[0]<<16|words[1] || binary.LittleEndian.Uint32(fixed[offset+4:]) != words[2]<<16|words[3] {
			t.Errorf("numeric VERSIONINFO does not match %s", numeric)
		}
	}
	for key, value := range versionStrings(version) {
		keyBytes := utf16Bytes(key)
		i := bytes.Index(versionInfo, keyBytes)
		if i < 0 {
			t.Errorf("VERSIONINFO missing %s", key)
			continue
		}
		start := (i + len(keyBytes) + 3) &^ 3
		if !bytes.HasPrefix(versionInfo[start:], utf16Bytes(value)) {
			t.Errorf("VERSIONINFO %s does not equal %q", key, value)
		}
	}
	group := resources[14][1] // RT_GROUP_ICON
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	if len(group) != 6+14*len(sizes) || binary.LittleEndian.Uint16(group[2:]) != 1 || int(binary.LittleEndian.Uint16(group[4:])) != len(sizes) {
		t.Fatal("missing seven-size icon group")
	}
	ico, err := os.ReadFile("../../internal/web/static/brand/murmur.ico")
	if err != nil {
		t.Fatal(err)
	}
	iconPayloads := map[byte][]byte{}
	for i, size := range sizes {
		icoEntry := ico[6+16*i : 6+16*(i+1)]
		if int(icoEntry[0]) != size%256 || int(icoEntry[1]) != size%256 {
			t.Fatalf("ICO entry %d is not %dx%d", i, size, size)
		}
		length, offset := binary.LittleEndian.Uint32(icoEntry[8:]), binary.LittleEndian.Uint32(icoEntry[12:])
		iconPayloads[byte(size%256)] = ico[offset : offset+length]
	}
	// go-winres orders group entries by resolution, independently of ICO order.
	for i := range sizes {
		entry := group[6+14*i : 6+14*(i+1)]
		payload, ok := iconPayloads[entry[0]]
		if !ok || entry[0] != entry[1] {
			t.Fatalf("unexpected or duplicate icon size %dx%d", entry[0], entry[1])
		}
		id := uint32(binary.LittleEndian.Uint16(entry[12:]))
		if !bytes.Equal(resources[3][id], payload) { // RT_ICON
			t.Errorf("icon %d does not match Murmur ICO payload", i)
		}
		delete(iconPayloads, entry[0])
	}
}
