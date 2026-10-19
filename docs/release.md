# Releasing Driga

The release tag is the single version source. Packaging strips its leading `v`
for `main.version` and archive names. `main.commit` is the full source revision;
`main.date` is that commit's UTC committer time, rather than the packaging clock.
`driga version` reports those fields, the Go toolchain, and OS/architecture.
`driga version --json` keeps the existing schema-2 envelope and adds `commit`,
`date`, `go_version`, `os`, and `arch` alongside `data.version`. Ordinary source builds
still identify as `dev`, with `unknown` commit/date. Version commands do not read
agent histories or create Driga data.

## Prerequisites and blockers

- **No top-level LICENSE exists.** The maintainer must choose and add it before
  distribution. Packaging refuses a normal release without it. Vendored font and
  JavaScript notices are included under `third-party/`; they do not license Driga.
- Complete every gate in README's **Validation and release** section, including
  independently labeled real samples from three machines, `real-second-machine`,
  a real Linux VM sample and full Linux VM test execution, binary growth, and
  representative footprint/CPU measurements. Existing skips and the open fleet
  RSS measurement remain blockers; packaging does not waive them.
- Obtain independent review before merge and preserve required commit/tag signing
  and repository checks. This change does not configure repository settings.
- Use Bash, Python 3.9+, Git, `shasum`, and exactly the Go version in `go.mod`
  (currently 1.27.2). Dependencies must be available in the Go module cache for
  offline builds. There are no network installs in the packaging scripts.
- SBOM generation needs preinstalled **Syft 1.33.0**. Optional local signing needs
  preinstalled **Cosign 2.6.0** and an owner-approved managed private-key mount.
  Neither tool is installed by these scripts. No new CI secrets are required by
  the workflow; signing keys and a trusted public-key distribution channel are
  not provisioned. If an encrypted signing key needs a password, inject
  `COSIGN_PASSWORD` at runtime without printing it or putting it in a file.

## Version policy

Tags must be unused `vMAJOR.MINOR.PATCH` SemVer syntax, optionally with prerelease
and build identifiers. As an explicit project choice, this CLI retains its
existing v1.x product stream and uses SemVer-compatible tags; Canon's workflow
**Release versions / Project choices** permits preserving existing streams.
Product versions do not change the CLI JSON schema or fleet protocol versions.
Any breaking public behavior needs migration notes alongside release notes.

Use `-alpha.1` and `-beta.1` for distributable prereleases, as Canon recommends.
The generic parser also accepts `v0.0.0-test` for packaging exercises. Keep
prereleases out of stable Homebrew and update channels, and mark them prereleases
if the maintainer later creates a GitHub release. Never replace a released tag or
its artifacts; fix forward with a new version.

## Local verification

From the repository root, use the project's cached-dependency sandbox setup:

```sh
rtk proxy env GOCACHE="$PWD/.superpowers/gocache" GOPROXY=off go vet ./...
rtk proxy env GOCACHE="$PWD/.superpowers/gocache" GOPROXY=off go test ./...
rtk proxy env GOCACHE="$PWD/.superpowers/gocache" GOPROXY=off go test -race ./...
rtk proxy gofmt -l cmd/driga/*.go internal/cli/root.go internal/cli/version_test.go
rtk proxy shellcheck scripts/release.sh scripts/release-evidence.sh
rtk proxy shfmt -d scripts/release.sh scripts/release-evidence.sh
rtk proxy actionlint .github/workflows/release.yml
rtk proxy env GOCACHE="$PWD/.superpowers/gocache" GOPROXY=off scripts/release.sh --dry-run v0.0.0-test
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
rtk proxy sh -c 'cd "$1" && shasum -a 256 -c SHA256SUMS' sh /path/to/driga-release-dry-run.XXXXXX
```

## Cut and qualify a release

1. Resolve the blockers above and finish review on a branch. Run the local
   checks, real-machine gates, and Linux VM tests; record measurements and skips.
   Have the maintainer merge the reviewed change following repository policy.
2. Have the maintainer create an unused signed release tag on the reviewed `main`
   commit and push that tag when authorized. For example, the next v1 stream
   release might be `v1.6.0`; this document does not reserve that number.
3. On a clean checkout of that existing tag, run:

   ```sh
   rtk proxy env GOPROXY=off scripts/release.sh v1.6.0
   rtk proxy scripts/release-evidence.sh sbom dist
   ```

   Normal packaging requires the tag to point at HEAD, rejects tracked and
   untracked changes, and refuses an existing `dist` path. Preserve earlier
   artifacts elsewhere before another normal build; it never wipes them.
   `dist/` is ignored by Git. Packaging itself does not execute the test gates.
4. Tag pushes run `.github/workflows/release.yml`: format, vet, full Linux tests,
   race tests, the same packaging script, checksum verification, and SBOM
   generation when the pinned Syft is provisioned. The default runner labels
   are `[self-hosted, Linux, X64, pixel]`, copied from Canon. If Pixel is
   unavailable, a maintainer can dispatch on the **same tag** with `hosted=true`
   for `ubuntu-24.04`. Dispatch on a branch intentionally does no packaging.
   The fallback requires no repository setting changes; Syft must still be
   provisioned before SBOM generation. A missing Syft is reported as a coverage
   gap in the run summary and must be resolved before distribution.
5. Download and retain the qualified CI artifact and run record. Compare
   `build-info.json` to the expected reviewed commit and tag; compare SHA256SUMS
   to the run summary and verify all files. If CI lacked Syft, generate SBOMs on
   those exact downloaded archives, retain the original run manifest, and record
   the new manifest. Do not rebuild binaries for publication. Have the maintainer
   sign the final manifest through an approved managed key mount if signing is
   required by their release policy:

   ```sh
   rtk proxy scripts/release-evidence.sh sign /absolute/path/to/qualified-output /absolute/path/to/managed/key
   ```

   The signature covers archives, build metadata and any generated SBOMs through
   SHA256SUMS. Signing refuses dry-run artifacts and existing signatures. It
   makes no transparency-log upload. Use the official 1Password MCP if an agent
   needs a 1Password developer Environment; never paste keys into commands,
   prompts or logs, or copy them into this repository.
6. Only after the manual gates pass may a maintainer publish those exact checked
   archives, final SHA256SUMS, build-info, SBOMs and optional signature. No script
   or workflow here creates a GitHub release, updates a tap, or publishes a module.
   Retain release artifacts beyond CI's 30-day artifact retention for rollback.
   A binary rollback does not undo database schema changes; review compatibility
   and backup/restore needs before changing installed versions.

The workflow uses read-only repository permission, no PR trigger, no stored
checkout credentials, no shared build cache, and full-SHA Action pins with version
comments. It verifies the tagged commit belongs to `main` before running project
checks. The run summary records build identity and checksums. `build-info.json`
is an unsigned build record, **not** a verified provenance attestation or a SLSA
claim. Canon's package guidance notes GitHub attestations may require Enterprise
Cloud for private repositories on Team. No new attestation service, OIDC trust,
repository environment or signing key is assumed here. SBOMs describe discoverable
binary dependencies and are not vulnerability scans or a complete license audit.

## Reproduce the archives

The shipped matrix is macOS (`darwin`) and Linux, each on amd64 and arm64. Windows
amd64/arm64 command binaries are compiled into a disposable intermediate directory
only. Their platform-specific entry point prints the stamped build identity and
an unsupported-platform error, then exits 1 without accessing data. This compiles
the command, not the Unix-dependent internal packages or a working Windows CLI.
Archives are `driga_<version>_<os>_<arch>.tar.gz` and contain `driga`, `README.md`,
`LICENSE`, and bundled asset license notices.

Builds use `CGO_ENABLED=0`, `-mod=readonly`, `-trimpath`, `-buildvcs=false`,
`-ldflags "-s -w -X main.version=... -X main.commit=... -X main.date=..."`,
GOAMD64=v1 and GOARM64=v8.0. User GOFLAGS and GOEXPERIMENT are cleared, workspaces
and automatic toolchain downloads are disabled. Archive contents have sorted
names, fixed permissions, zero owners, commit-time tar mtimes, and zero gzip
mtime with no filename. Rebuild the same tag using the same toolchain, source,
dependency checksums, and Python/zlib versions, then compare archive hashes.
Optional SBOMs and signatures may carry tool-generated metadata and are outside
the byte-reproducible archive claim.

## Verify and install a release archive

Download the archive for your OS/architecture plus its manifest and build record
from the maintainer's trusted release location. Check the expected version,
reviewed commit, builder/run, and release gates before installation. Checksums
detect corruption; authenticating an attacker-supplied manifest requires an
independently trusted signing key or provenance identity.

With a signing key already obtained through a trusted channel, optional offline
signature verification is:

```sh
rtk proxy cosign verify-blob --key /trusted/path/driga.pub --signature SHA256SUMS.sig --insecure-ignore-tlog SHA256SUMS
```

The tlog option is appropriate only for this explicitly key-based offline
signature, which does not have a transparency-log entry. An attestation added
later must be verified against the expected repository, source revision and
builder identity, not merely accepted because a verification command succeeds.

If all manifest files were downloaded, run `rtk proxy shasum -a 256 -c SHA256SUMS`.
For one platform, select only that exact filename from the authenticated manifest:

```sh
# Example for macOS/arm64; substitute the intended release and platform.
rtk proxy sh -c 'awk '\''$2 == "driga_1.6.0_darwin_arm64.tar.gz" { print; found=1 } END { if (!found) exit 1 }'\'' SHA256SUMS > selected-checksum.txt && shasum -a 256 -c selected-checksum.txt'
rtk proxy mkdir -p release-unpacked
rtk proxy tar -xzf driga_1.6.0_darwin_arm64.tar.gz -C release-unpacked
rtk proxy mkdir -p "$HOME/.local/bin"
rtk proxy install -m 0755 release-unpacked/driga "$HOME/.local/bin/driga"
rtk proxy "$HOME/.local/bin/driga" version --json
```

Keep Git and `~/.local/bin` on PATH and the installed binary at a stable absolute path
before configuring hooks or a service. Retain the archive and its LICENSE.

## Install from source and Homebrew template

After the module is published and reachable, the source installation path is:

```sh
rtk proxy env CGO_ENABLED=0 go install github.com/labstack/driga/cmd/driga@latest
# Or pin a published tag:
rtk proxy env CGO_ENABLED=0 go install github.com/labstack/driga/cmd/driga@v1.6.0
```

This compiles from source using Go 1.27.2 or newer; it does not download a release
archive or apply the release linker stamps, so `version` remains `dev`. With the
current unsuffixed module path, source installation tags must remain v0/v1 under
Go's module major-version rules. From an offline checkout, use the README's
`CGO_ENABLED=0 go build -o driga ./cmd/driga` command with cached dependencies.

`packaging/homebrew/driga.rb` is an unpublished template. Replace its version and
all four SHA256 placeholders using the final verified manifest, set the license
only after the maintainer chooses it, and review/test in a separately managed tap.
It installs prebuilt macOS/Linux archives and needs no Go build dependency.
There is no Homebrew publication automation in this repository.

## Canon mapping

- `conventions/workflow.md`: **Rules**, **Release security**, **Release versions**,
  **Project choices** — focused branch, review before merge, immutable tags,
  source/check linkage, minimal permissions, pinned Actions, exact-artifact
  promotion, optional SBOM/signing, explicit existing version stream.
- `README.md` and `.github/workflows/typescript.yml`: Pixel default, explicit
  hosted fallback, checkout/upload Action pins, and no shared release caches.
- `conventions/tooling.md`: **Rules / Verification** — Go version from go.mod,
  pinned optional tools, same local/CI packaging commands, reported coverage gaps.
- `conventions/security.md`: **Rules** — no secret values in source/output;
  managed mounts/runtime injection for signing material.
- `conventions/cli.md`: **Rules / Verification** — schema-2 JSON, diagnostics on
  stderr, tests for version success and output write failures.
- `stack/backend.md`: **Backend profiles / Rules** — preserve the existing Go
  CLI and dependency architecture; do not replace Kong with another framework.
- `packages/README.md`: **Versions, tags, and releases / Writing a package** —
  consulted for tag immutability and current private-repo attestation limitations;
  shared-package manifests and CalVer rules do not apply to this CLI.

Upstream references: [setup-go v5.5.0](https://github.com/actions/setup-go/releases/tag/v5.5.0),
[Syft v1.33.0](https://github.com/anchore/syft/releases/tag/v1.33.0), and
[Cosign blob signing](https://docs.sigstore.dev/cosign/signing/signing_with_blobs/).
