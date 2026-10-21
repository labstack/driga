# Releasing Driga

The release tag is the single version source. Packaging strips its leading `v`
for `main.version` and archive names. `main.commit` is the full source revision;
`main.date` is that commit's UTC committer time, rather than the packaging clock.
`driga version` reports those fields, the Go toolchain, and OS/architecture.
`driga version --json` keeps the existing schema-2 envelope and adds `commit`,
`date`, `go_version`, `os`, and `arch` alongside `data.version`. Ordinary source builds
still identify as `dev`, with `unknown` commit/date. Version commands do not read
agent histories or create Driga data.

## Version policy

Driga is in alpha. Tags use **`vYYYY.M.REVISION-alpha`**, for example
`v2026.10.0-alpha`. The month has no leading zero. `REVISION` starts at `0`
each UTC month and increments for each release in that month. The `-alpha`
suffix is optional in the validator to allow stable releases later.

The script and workflow use this validation pattern:

```text
^v[1-9][0-9]{3}\.([1-9]|1[0-2])\.(0|[1-9][0-9]{0,18})(-alpha)?$
```

Choose an unused tag for the current UTC month and the next revision. Validation
checks syntax; the maintainer checks the month and revision against prior tags.
Never replace a released tag or its artifacts. Product versions do not change
the CLI JSON schema or fleet protocol version. Breaking behavior needs migration
notes in the release notes.

Because the major number is the year, `go install ...@<tag>` does not work with
the unsuffixed `github.com/labstack/driga` module path under Go's module
major-version rules. Install from release archives or build a source checkout.

## Prerequisites

- Use Bash, Python 3.9+, Git, `shasum`, and exactly the Go version in `go.mod`.
  Dependencies must be cached for offline builds; the scripts install no tools.
- Run formatting, vet, tests, race tests, and a cgo-free build. Review skipped
  platform checks and exercise macOS, Linux, and Windows before publishing.
  On Windows verify per-user task installation/logon/restart, hook delivery,
  terminal launch, and plain toast delivery with a real agent session.
- Driga is licensed under Apache-2.0. Packaging requires `LICENSE` and ships
  `NOTICE` and bundled font/JavaScript license notices.
- Optional SBOM generation needs preinstalled **Syft 1.33.0**. Optional signing
  needs preinstalled **Cosign 2.6.0** and a managed private-key mount. Supply any
  `COSIGN_PASSWORD` through runtime injection; keep keys and passwords out of
  source, command output, and temporary files.

## Local verification

From the repository root, isolate test state before running checks:

```sh
task_root=$(mktemp -d)
mkdir -p "$task_root/home" "$task_root/data" "$task_root/spool"
export HOME="$task_root/home"
export CODEX_HOME="$HOME/.codex" CLAUDE_CONFIG_DIR="$HOME/.claude"
export COPILOT_HOME="$HOME/.copilot" GEMINI_CLI_HOME="$HOME/.gemini"
export XDG_CONFIG_HOME="$HOME/.config" XDG_DATA_HOME="$HOME/.local/share"
export XDG_STATE_HOME="$HOME/.local/state"
export DRIGA_DATA_DIR="$task_root/data" DRIGA_SPOOL_DIR="$task_root/spool"
export DRIGA_CONFIG="$task_root/config.toml"
# Preserve an existing module cache if needed for offline builds.
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
actionlint .github/workflows/*.yml
GOPROXY=off bash scripts/release.sh --dry-run v2026.10.0-alpha
```

The last command really builds all targets, including on a dirty tree, into a
new temporary directory. It prints `DRY RUN — NON-DISTRIBUTABLE` and a final
`DIST_DIR=...` path. That directory is retained for inspection; only the script's
own intermediate directory is removed. Dry-run archives carry
`NON-DISTRIBUTABLE.txt`, and dirty source is stamped with `<commit>-dirty`.
A missing LICENSE is omitted only in this mode; no placeholder license is chosen.
Checksums are generated and verified automatically. To independently check them:

```sh
# Substitute the exact DIST_DIR printed by the dry run.
sh -c 'cd "$1" && shasum -a 256 -c SHA256SUMS' sh /path/to/driga-release-dry-run.XXXXXX
```

## Cut and qualify a release

1. Review the change and run the checks above. Record platform coverage and
   any skips, then merge through the repository's normal review process.
2. Choose the next calendar version, create a signed tag on the reviewed `main`
   commit, and push it. For example, `v2026.10.0-alpha` is October 2026's first
   alpha release; this document does not reserve that number.
3. On a clean checkout of the existing tag, run:

   ```sh
   GOPROXY=off scripts/release.sh v2026.10.0-alpha
   scripts/release-evidence.sh sbom dist
   ```

   Normal packaging requires the tag to point at HEAD, rejects tracked and
   untracked changes, and refuses an existing `dist` path. Preserve earlier
   artifacts elsewhere. Packaging does not run the tests itself.
4. Tag pushes run `.github/workflows/release.yml` on GitHub-hosted
   `ubuntu-24.04`. A calendar-shaped tag trigger is followed by an exact regex
   guard, including for manual dispatch on a tag. Dispatch on a branch does no
   packaging. The workflow checks ancestry on `main`, formatting, vet, Linux
   tests, race tests, packaging, and checksums. It generates SBOMs when Syft is
   provisioned and reports missing coverage otherwise.
5. Download the qualified artifact and retain its run record. Compare
   `build-info.json` to the reviewed commit and tag, and verify SHA256SUMS. If
   CI lacked Syft, generate SBOMs on those exact archives before publication.
   Do not rebuild binaries for publication. If signing is used, sign the final
   manifest through a managed key mount:

   ```sh
   scripts/release-evidence.sh sign /absolute/path/to/qualified-output /absolute/path/to/managed/key
   ```

   The signature covers archives, metadata, and SBOMs through SHA256SUMS.
   Signing refuses dry-run artifacts and existing signatures and makes no
   transparency-log upload.
6. Publish those exact archives, SHA256SUMS, build-info, SBOMs, and optional
   signature. **Mark any `-alpha` GitHub release as a prerelease.** No workflow
   or script here creates a GitHub release, updates a tap, or publishes a
   module. Retain published artifacts beyond CI's 30-day retention.

The workflow has read-only repository permission, pinned Actions, no PR trigger,
no stored checkout credentials, and no shared build cache. `build-info.json` is
an unsigned build record, not a verified provenance attestation. SBOMs describe
discoverable dependencies; they are not vulnerability scans or complete license
audits. A binary rollback does not undo database migrations; review data
compatibility before changing installed versions.

## Reproduce the archives

The shipped matrix is macOS (`darwin`), Linux, and experimental Windows, each on
amd64 and arm64. macOS/Linux archives are
`driga_<version>_<os>_<arch>.tar.gz`; Windows archives are
`driga_<version>_windows_<arch>.zip`. Each contains `driga` (or `driga.exe`),
`README.md`, `LICENSE`, `NOTICE`, `third-party/vendor-LICENSES.txt`, and
`third-party/fonts-LICENSE.txt`. All six targets appear in `build-info.json` and
`SHA256SUMS`; there are no compile-only targets. SBOM generation covers ZIPs too.

Builds use `CGO_ENABLED=0`, `-mod=readonly`, `-trimpath`, `-buildvcs=false`,
`-ldflags "-s -w -X main.version=... -X main.commit=... -X main.date=..."`,
GOAMD64=v1 and GOARM64=v8.0. User GOFLAGS and GOEXPERIMENT are cleared, workspaces
and automatic toolchain downloads are disabled. Archive contents have sorted
names, fixed permissions, zero owners, commit-time tar mtimes, and zero gzip
mtime with no filename. ZIP entries use commit-time UTC DOS timestamps
(two-second resolution, clamped to 1980), fixed Unix mode attributes and deflate
level 9. Rebuild the same tag using the same toolchain, source,
dependency checksums, and Python/zlib versions, then compare archive hashes.
Optional SBOMs and signatures may carry tool-generated metadata and are outside
the byte-reproducible archive claim.

## Verify and install a release archive

Download the archive for your OS/architecture plus its manifest and build record
from [GitHub Releases](https://github.com/labstack/driga/releases). Check the
expected version, reviewed commit, and builder/run before installation. Checksums
detect corruption; authenticating an attacker-supplied manifest requires an
independently trusted signing key or provenance identity.

With a signing key already obtained through a trusted channel, optional offline
signature verification is:

```sh
cosign verify-blob --key /trusted/path/driga.pub --signature SHA256SUMS.sig --insecure-ignore-tlog SHA256SUMS
```

The tlog option is appropriate only for this explicitly key-based offline
signature, which does not have a transparency-log entry. An attestation added
later must be verified against the expected repository, source revision and
builder identity, not merely accepted because a verification command succeeds.

If all manifest files were downloaded, run `shasum -a 256 -c SHA256SUMS`.
For one platform, select only that exact filename from the authenticated manifest:

```sh
# Example for macOS/arm64; substitute the intended release and platform.
sh -c 'awk '\''$2 == "driga_2026.10.0-alpha_darwin_arm64.tar.gz" { print; found=1 } END { if (!found) exit 1 }'\'' SHA256SUMS > selected-checksum.txt && shasum -a 256 -c selected-checksum.txt'
mkdir -p release-unpacked
tar -xzf driga_2026.10.0-alpha_darwin_arm64.tar.gz -C release-unpacked
mkdir -p "$HOME/.local/bin"
install -m 0755 release-unpacked/driga "$HOME/.local/bin/driga"
"$HOME/.local/bin/driga" version --json
```

Keep Git and `~/.local/bin` on PATH and the installed binary at a stable absolute path
before configuring hooks or a service. Retain the archive and its LICENSE.

On Windows, use `Get-FileHash -Algorithm SHA256` to compare the ZIP's hash with
its exact entry in the authenticated `SHA256SUMS`, then extract the ZIP with
Explorer or `Expand-Archive`. Put `driga.exe` in a stable directory on PATH and
run `driga version --json`, then `driga service install` and `driga doctor`.
PowerShell here is an optional installation shell; Driga does not invoke it for
tasks, alerts or terminal launch. Windows remains experimental (phase 2).

## Install from source and Homebrew template

Build a source checkout using the Go version in `go.mod` or newer:

```sh
CGO_ENABLED=0 go build ./cmd/driga
./driga version
```

An ordinary source build reports `dev`; it does not apply the release linker
stamps. To build a specific release, check out its tag first. `go install` with
a CalVer tag is incompatible with the module path's major-version rules, as
explained above.

`packaging/homebrew/driga.rb` is an unpublished template. Replace its version and
all four SHA256 placeholders using the final verified manifest, keep the
Apache-2.0 license, and review/test in a separately managed tap. It installs
prebuilt macOS/Linux archives and needs no Go build dependency. There is no
Homebrew publication automation in this repository.
