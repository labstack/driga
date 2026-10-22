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
always enable macOS signing and notarization, even when credentials are absent,
so missing credentials fail the release instead of publishing unsigned binaries.
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
  `/usr/bin/driga` for amd64 and arm64.
- `driga_<version>_checksums.txt`, a SHA-256 manifest of all ten downloads.
- `Casks/driga.rb` in `labstack/homebrew-tap`, installing the signed and notarized
  macOS binaries, and `driga.json` in `labstack/scoop-bucket` for Windows.

Each archive contains `driga` (or `driga.exe`), `README.md`, `LICENSE`, `NOTICE`,
`third-party/vendor-LICENSES.txt`, and `third-party/fonts-LICENSE.txt`. The last
two preserve the bundled web vendor and font license notices. Builds disable
cgo, use `-trimpath`, and embed version, full commit, and commit date.
The package metadata names LabStack, `support@labstack.com`, and Apache-2.0.
Changelogs sort ascending and omit `docs:` and `test:` entries.

GoReleaser replaces the old packaging/evidence scripts and manual Homebrew
template. This flow does not generate the former optional Syft SBOMs or Cosign
manifest signatures. macOS code signing and notarization are required instead;
SHA-256 manifests detect corruption but do not authenticate their own origin.

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
checksums, every archive's files, Debian binary installation path when
`dpkg-deb` is available, and the Homebrew/Scoop versions, URLs, and hashes.
It runs only the host binary's safe `version --json` command, checks its version
against the tag without `v` (or the snapshot version), and requires embedded
commit/date metadata. Verification hosts are macOS or Linux on amd64/arm64.
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
install -m 0755 release-unpacked/driga "$HOME/.local/bin/driga"
"$HOME/.local/bin/driga" version --json
```

If all ten artifacts are downloaded, `sha256sum -c driga_<version>_checksums.txt`
(or `shasum -a 256 -c`) checks them together. On Windows, compare the ZIP with
its exact checksum entry using `Get-FileHash -Algorithm SHA256`, then use
`Expand-Archive` and place `driga.exe` in a stable directory on PATH.

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
