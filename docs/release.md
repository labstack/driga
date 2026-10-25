# Releasing Driga

Driga uses the same tag-driven GoReleaser flow as Onebox. The release script
creates a tag; GitHub Actions verifies the source, publishes the prerelease,
and updates Homebrew and Scoop. Windows remains experimental.

## Version policy

Tags use **`vYYYY.M.REVISION-alpha`**, for example `v2026.10.0-alpha`.
The month has no leading zero, and revision starts at zero each UTC month.
`scripts/release.sh` chooses the next unused revision from fetched tags.
The validator also accepts a stable tag for future use:

```text
^v[1-9][0-9]{3}\.([1-9]|1[0-2])\.(0|[1-9][0-9]{0,18})(-alpha)?$
```

The workflow currently triggers only on `v*.*.*-alpha`. All releases are marked
as prereleases and do not change GitHub's latest-release pointer. A future
stable release needs an intentional trigger and release-policy update.
Never replace a published tag or its artifacts. If publication partially fails,
wait for the run to finish and cut the next revision to repair it.

The tag is the version source. GoReleaser strips its leading `v` for
`main.version` and artifact names, embeds the full source commit in `main.commit`,
and uses the commit date for `main.date`. `driga version` prints the version,
commit, date, Go toolchain, and OS/architecture. `driga version --json` preserves
the schema-2 envelope with those fields under `data`. Source builds report
`dev` and `unknown` commit/date. The version command does not collect agent data.

## Required credentials and publication targets

Configure these repository Actions secrets before a real release:

| Secret | Purpose |
| --- | --- |
| `PACKAGE_REPOS_TOKEN` | Contents write/push access to `labstack/homebrew-tap` and `labstack/scoop-bucket`, both with default branch `main`. |
| `MACOS_SIGN_P12` | Base64-encoded Developer ID Application signing certificate in PKCS#12 format. |
| `MACOS_SIGN_PASSWORD` | Password for that signing certificate. |
| `MACOS_NOTARY_ISSUER_ID` | App Store Connect API issuer ID. |
| `MACOS_NOTARY_KEY_ID` | App Store Connect API key ID. |
| `MACOS_NOTARY_KEY` | Base64-encoded App Store Connect `.p8` private key. |

GitHub supplies `GITHUB_TOKEN`; the release job grants it `contents: write`
for publishing GitHub Releases. Other jobs have read-only repository access.
The release preflight refuses empty secrets and checks the package token's
push access and both publication repositories' default branches. Real releases
always require macOS app signing and notarization, even when credentials are absent,
so missing credentials fail the release instead of publishing unsigned apps.
Keep credential values in the secrets store; do not put them in commands,
release notes, or committed files.

The maintainer running the release script needs Git push access to `main` and
tags, plus authenticated `gh` read access to Actions runs. The script accepts
GitHub HTTPS or SSH origins; `DRIGA_RELEASE_REPOSITORY=labstack/driga` can name
the repository explicitly for other remote URL forms. Branch protection must
permit the release identity's metadata-only fast-forward of `main`.

## Cut a release

1. Merge reviewed changes to `main`, update your checkout, and ensure it is clean,
   including untracked files. Use Bash, Git, `gh`, and the Go version in `go.mod`.
2. From the repository root on `main`, run:

   ```sh
   scripts/release.sh
   ```

   This fetches main and tags and requires HEAD to equal `origin/main`. It checks
   `gofmt -l .`, runs `go vet ./...` and `go test ./...` using disposable agent
   and Driga directories, then revalidates the checkout and remote main. It
   refuses to start while the previous release workflow is still running or
   its status cannot be read. Local test repositories use local remotes only.
3. The script creates a metadata-only release commit with the checked source
   tree, tags it with the next calendar revision, and atomically pushes main
   and the tag under a lease on the checked main commit. An advanced main,
   competing tag, or branch-policy refusal publishes neither update and removes
   the local candidate tag. A successful push advances local main too.
4. Watch the [Release workflow](https://github.com/labstack/driga/actions/workflows/release.yml).
   Its `verify` job calls the existing CI workflow, including **Tests and build**
   and **Windows tests**. Publication waits for both jobs to succeed.
5. The release job fetches lineage, validates the tag at HEAD and its ancestry on
   main, refuses an already-published or older release, checks credentials and
   publication targets, and runs GoReleaser **v2.18.0** with `release --clean`.
   It verifies the resulting distribution after publication. Releases share a
   global queue, protecting the shared package repositories.

A completed workflow does not replace platform qualification: Windows is
experimental until real sessions, service logon/restart, hooks, terminal launch,
and notifications have been verified on hardware. Pre-release databases must be reset; `driga doctor` prints the reset command.

## What gets published

- Six archives for linux/darwin/windows on amd64/arm64:
  `driga_<version>_<os>_<arch>.tar.gz`, with `.zip` for Windows.
- Four Linux packages: `driga_<version>_linux_<arch>.deb` and `.rpm`, installing
  `/usr/bin/driga`, `driga.desktop` and the application icon for amd64 and
  arm64. They recommend (not require) GTK 3, WebKitGTK 4.1 and an AppIndicator
  library: `libgtk-3-0`, `libwebkit2gtk-4.1-0` and `libayatana-appindicator3-1`
  on Debian, `gtk3`, `webkit2gtk4.1` and `libayatana-appindicator-gtk3` on RPM.
- `driga_<version>_checksums.txt`, a SHA-256 manifest of all ten downloads.
- `Casks/driga.rb` in `labstack/homebrew-tap`, installing the signed and notarized
  macOS app bundles, and `driga.json` in `labstack/scoop-bucket` for Windows.

Each archive contains `driga` (macOS uses `Driga.app/Contents/MacOS/driga`; the
Windows archive has `Driga.exe`, the GUI built from `cmd/driga-app` with
`-H=windowsgui`, at its root and the CLI as `bin/driga.exe`), `README.md`, `LICENSE`, `NOTICE`,
`third-party/vendor-LICENSES.txt`, and `third-party/fonts-LICENSE.txt`. The last
two preserve the bundled web vendor and font license notices. Builds disable
cgo, use `-trimpath`, and embed version, full commit, and commit date.
The package metadata names LabStack, `support@labstack.com`, and Apache-2.0.
Changelogs sort ascending and omit `docs:` and `test:` entries.

### Windows executable resources

The global GoReleaser before hook runs `go run ./scripts/windows-resources
-version <version>` once before parallel builds. It uses the pure-Go
`github.com/tc-hib/go-winres` tool pinned to **v0.3.3**, so cross-builds require
neither cgo nor a Windows SDK. It generates `rsrc_windows_amd64.syso` and
`rsrc_windows_arm64.syso` in both `cmd/driga` and `cmd/driga-app`, so `Driga.exe`
and `bin/driga.exe` share the icon and version resources; Go's filename constraints include each object
only in its matching Windows build. These deterministic, version-specific
objects are ignored rather than committed, avoiding stale release metadata.

The icon is the canonical Murmur symbol in `internal/web/static/brand/murmur.ico`,
checked in with 16, 24, 32, 48, 64, 128 and 256 pixel images. Regenerate it with
`python3 scripts/generate-brand.py` using `rsvg-convert` (librsvg); repeated runs
with the same renderer produce identical bytes, without timestamps. Release
builds consume the ICO directly and need neither Python nor librsvg.

VERSIONINFO identifies Driga and LabStack LLC, using the copyright line from
NOTICE. FileVersion and ProductVersion retain the full release/snapshot string.
Their numeric fields map `2026.10.5-alpha` to `2026.10.5.0`; Windows' 16-bit
component limit makes revisions above 65535 a build error. Development resources
use `dev` and `0.0.0.0`. For an optional branded source build on Windows, run
`go generate ./cmd/driga` before `go build ./cmd/driga`. Plain `go build` also
works without generated objects. Regenerate after switching versions.

GoReleaser replaces the old packaging/evidence scripts and manual Homebrew
template. This flow does not generate the former optional Syft SBOMs or Cosign
manifest signatures. macOS code signing and notarization are required instead;
SHA-256 manifests detect corruption but do not authenticate their own origin.

## macOS bundle and signing design

Each Darwin architecture has a minimal `Driga.app` containing `Contents/Info.plist`,
`Contents/MacOS/driga`, and `Contents/Resources/Driga.icns`. Its identifier is
`run.driga`, name/display name is Driga, executable is `driga`, and package type
is `APPL`. The one executable is both the app and the CLI. `LSEnvironment`
sets `DRIGA_APP=1`, which is how Finder, Dock, Spotlight and login-item launches
open the app (the CLI checks it; macOS has no other login detection), and
`NSHighResolutionCapable` is true. There is no `LSUIElement`: the app switches
its activation policy at run time. See Apple's
[Launch Services keys](https://developer.apple.com/library/archive/documentation/General/Reference/InfoPlistKeyReference/Articles/LaunchServicesKeys.html).
The structure follows Apple's
[Bundle Programming Guide](https://developer.apple.com/library/archive/documentation/CoreFoundation/Conceptual/CFBundles/BundleTypes/BundleTypes.html).
Both standard version keys use the tag's three numeric CalVer components;
`DrigaReleaseVersion` and the CLI retain the full alpha/snapshot version. This
respects Apple's numeric
[version-key formats](https://developer.apple.com/library/archive/documentation/General/Reference/InfoPlistKeyReference/Articles/CoreFoundationKeys.html).

The committed icon preserves the canonical Murmur geometry and color. Regenerate
it on macOS with `python3 scripts/generate-brand.py --icns-only` (`rsvg-convert`
and `iconutil` required). The generator renders [Apple's complete iconset](https://developer.apple.com/library/archive/documentation/GraphicsAnimation/Conceptual/HighResolutionOSX/Optimizing/Optimizing.html):
16, 32, 128, 256 and 512 point representations at 1x/2x. `--portable-icns` explicitly encodes those same PNGs
when ImageIO/iconutil is unavailable in a sandbox. The output has no timestamps;
Linux CI consumes the committed ICNS and validates every representation.

[Apple's helper documentation](https://developer.apple.com/documentation/servicemanagement/updating-helper-executables-from-earlier-versions-of-macos)
explains the organization-name fallback and `AssociatedBundleIdentifiers`.
On a headless Mac, `service install` resolves a bundled executable, associates `run.driga`, registers
the app with Launch Services, and atomically repairs an existing stale plist.
It stops a running collector before replacing its stale definition; the CLI
then starts it. Reinstalling a current definition is harmless. Development
binaries outside the app retain their ordinary path and have no association.
The executable and app have the same signing team because the CLI is the app's
main executable. Registration matters: Apple requires Launch Services to know
the associated app.

The entire release job runs on GitHub-hosted `macos-15`. An OSS per-build post
hook assembles the app, then `codesign --options runtime --timestamp` signs the
whole bundle with a Developer ID Application identity and seals its resources.
Apple documents the [hardened-runtime and timestamp requirements](https://developer.apple.com/documentation/security/resolving-common-notarization-issues).
It submits a temporary ZIP through `notarytool`, requires Accepted, retrieves
the log, staples and validates the app, and verifies its signature and Gatekeeper
assessment. Only then does GoReleaser archive it, compute checksums and publish.
The published tarballs are extracted and checked again for signatures and
staples. Linux/Windows archives, nFPM, Homebrew and Scoop remain in this same
GoReleaser flow, preserving the serialized, immutable-release policy.

The temporary keychain contains only the imported Application identity and the
notary profile. Decoded certificate/API-key files are private and removed at
setup exit; an `always()` workflow step deletes the keychain and signing directory
even after failure. No Installer certificate or extra signing secret is needed.
See Apple's [custom notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow):
ZIP is a submission format, the ticket is stapled to the app before final
packaging, and standalone binaries cannot have tickets stapled to them.

GoReleaser **v2.18.0 OSS** supports
[per-build hooks](https://github.com/goreleaser/goreleaser/blob/v2.18.0/www/content/customization/builds/hooks.md),
[archive paths/files](https://github.com/goreleaser/goreleaser/blob/v2.18.0/www/content/customization/package/archives.md),
and [`homebrew_casks.binaries`](https://github.com/goreleaser/goreleaser/blob/v2.18.0/www/content/customization/publish/homebrew_casks.md).
Its [notarization documentation](https://github.com/goreleaser/goreleaser/blob/v2.18.0/www/content/customization/sign/notarize.md)
limits the OSS quill integration to Mach-O/universal binaries. Native app
notarization, `app_bundles`, DMG packaging, `homebrew_casks.app`, archive hooks,
and templated archive files are Pro-only and are not used here. A build hook
creates the real plist instead of relying on Pro templated files.

The OSS cask `custom_block` emits Homebrew's normal app artifact, `app "Driga.app"`,
which installs the bundle into `/Applications`. The `binaries` setting emits
`binary "#{appdir}/Driga.app/Contents/MacOS/driga"`, linking the CLI into
`$(brew --prefix)/bin/driga` from inside the installed app. This is ordinary
Ruby cask generation, not GoReleaser's Pro `app`/DMG feature. The caveats
describe opening the app and no longer mention `driga service install`: the
app collects in the background and registers its own login item on first
launch. A launchd service from an earlier release keeps the collector lock; the
app shows it as the collector until `driga service uninstall` hands collection
to the app.

A real signed release and macOS installation are still needed to qualify
Gatekeeper, offline staple validation, Homebrew upgrade and app relaunch, and
the actual Driga name/Murmur icon in Settings. macOS may retain older cached
background entries; no automated BTM reset is performed.

## Local release verification

With GoReleaser v2.18.0 and the Go version in `go.mod` already installed:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish,sign,notarize
scripts/verify-release-dist.sh dist
```

Snapshots build local artifacts and package metadata without publishing. Their
synthetic version differs from the current Git tag; they are for inspection.
Unsigned snapshots need no publication credentials. `--clean` replaces `dist`.
CI runs the same configuration check, snapshot build, and artifact inspection
inside **Tests and build**, as well as its existing tests and cross-builds.

The verifier checks the exact six-archive/four-package inventory, complete
checksums, every archive's files and the macOS app metadata/icon size set, Debian binary installation path when
`dpkg-deb` is available, and the Homebrew/Scoop versions, URLs, and hashes.
It runs only the host binary's safe `version --json` command, checks its version
against the tag without `v` (or the snapshot version), and requires embedded
commit/date metadata. Verification hosts are macOS or Linux on amd64/arm64.
It also uses Go's PE parser to inspect both archived Windows executables for
the numeric versions, full identity strings, and all seven Murmur icon images.
The resource generator tests compare Linux/macOS binaries before and after
adding the objects and require identical bytes on both architectures.
The Go tests under `scripts/` use disposable repositories and fake commands,
never real agent CLIs or application data.

## Verify and install a release archive

Homebrew on macOS:

```sh
brew install --cask labstack/tap/driga
```

Scoop on experimental Windows:

```powershell
scoop bucket add labstack https://github.com/labstack/scoop-bucket
scoop install labstack/driga
```

The manifest shims only `bin/driga.exe` and adds a Start menu shortcut to
`Driga.exe`. The app is a plain archive file, not an archive binary, because a
`Driga.exe` shim would collide with `driga.exe` on case-insensitive Windows and
make `driga` open the app.

For manual installation, download the archive or Linux package for your
OS/architecture and `driga_<version>_checksums.txt` from
[GitHub Releases](https://github.com/labstack/driga/releases). Check the expected
release and repository before trusting the manifest. For example, with only
the macOS/arm64 archive downloaded:

```sh
version=2026.10.0-alpha
archive="driga_${version}_darwin_arm64.tar.gz"
awk -v archive="$archive" '$2 == archive { print; found=1 } END { if (!found) exit 1 }' \
  "driga_${version}_checksums.txt" > selected-checksum.txt
shasum -a 256 -c selected-checksum.txt
mkdir -p release-unpacked
tar -xzf "$archive" -C release-unpacked
mkdir -p "$HOME/.local/bin"
mkdir -p "$HOME/.local/lib/driga"
mv release-unpacked/Driga.app "$HOME/.local/lib/driga/Driga.app"
ln -s "$HOME/.local/lib/driga/Driga.app/Contents/MacOS/driga" "$HOME/.local/bin/driga"
"$HOME/.local/bin/driga" version --json
```

If all ten artifacts are downloaded, `sha256sum -c driga_<version>_checksums.txt`
(or `shasum -a 256 -c`) checks them together. On Windows, compare the ZIP with
its exact checksum entry using `Get-FileHash -Algorithm SHA256`, then use
`Expand-Archive`, keep `Driga.exe` and its `bin` directory together in a stable
directory, and put that `bin` directory on PATH.

After verifying a Linux package, install it with
`sudo dpkg -i driga_<version>_linux_<arch>.deb` or
`sudo rpm -Uvh driga_<version>_linux_<arch>.rpm`. Keep Git and the installed
binary on PATH at a stable absolute path before setting up hooks or a service.
Retain the archive's license notices.

## Install from source

Use a source checkout and the Go version in `go.mod` or newer:

```sh
CGO_ENABLED=0 go build ./cmd/driga
./driga version
```

Check out a release tag first to build that source. Ordinary builds still report
`dev` without linker stamps. CalVer's year is the major number, so Go module
major-version rules prevent `go install ...@<tag>` with this unsuffixed module
path. Use a package, archive, or source checkout.
