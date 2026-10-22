#!/usr/bin/env bash
set -euo pipefail

dist_dir=${1:-dist}
metadata=${dist_dir}/metadata.json
if [ ! -f "$metadata" ]; then
  echo "release metadata is missing: ${metadata}" >&2
  exit 1
fi

release_version=$(jq -r '.version // empty' "$metadata")
release_tag=$(jq -r '.tag // empty' "$metadata")
if [ -z "$release_version" ] || [ -z "$release_tag" ]; then
  echo "release metadata has no version or tag." >&2
  exit 1
fi

expected_artifacts=(
  "driga_${release_version}_checksums.txt"
  "driga_${release_version}_darwin_amd64.tar.gz"
  "driga_${release_version}_darwin_arm64.tar.gz"
  "driga_${release_version}_linux_amd64.deb"
  "driga_${release_version}_linux_amd64.rpm"
  "driga_${release_version}_linux_amd64.tar.gz"
  "driga_${release_version}_linux_arm64.deb"
  "driga_${release_version}_linux_arm64.rpm"
  "driga_${release_version}_linux_arm64.tar.gz"
  "driga_${release_version}_windows_amd64.zip"
  "driga_${release_version}_windows_arm64.zip"
)

actual_artifacts=()
while IFS= read -r artifact; do
  actual_artifacts+=("$artifact")
done < <(find "$dist_dir" -maxdepth 1 -type f -name 'driga_*' -exec basename {} \; | LC_ALL=C sort)
if [ "${actual_artifacts[*]}" != "${expected_artifacts[*]}" ]; then
  echo "release artifact inventory differs from the contract." >&2
  diff -u <(printf '%s\n' "${expected_artifacts[@]}") <(printf '%s\n' "${actual_artifacts[@]}") >&2 || true
  exit 1
fi

checksum_file="${dist_dir}/driga_${release_version}_checksums.txt"
# By name, not by count: sha256sum --check passes on a manifest that lists one
# artifact twice and omits another, and so would any line count.
distributable_artifacts=()
for artifact in "${expected_artifacts[@]}"; do
  [ "$artifact" = "driga_${release_version}_checksums.txt" ] || distributable_artifacts+=("$artifact")
done
checksummed_artifacts=$(sed 's/^[0-9a-f]* [ *]*//' "$checksum_file" | LC_ALL=C sort)
if [ "$checksummed_artifacts" != "$(printf '%s\n' "${distributable_artifacts[@]}" | LC_ALL=C sort)" ]; then
  echo "checksum manifest does not cover exactly the distributable artifacts." >&2
  diff -u <(printf '%s\n' "${distributable_artifacts[@]}" | LC_ALL=C sort) <(printf '%s\n' "$checksummed_artifacts") >&2 || true
  exit 1
fi
distributable_count=${#distributable_artifacts[@]}
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist_dir" && sha256sum --check "$(basename "$checksum_file")")
else
  (cd "$dist_dir" && shasum -a 256 -c "$(basename "$checksum_file")")
fi

# Every later hash comparison reads this manifest rather than recomputing: the
# bytes behind it were just checked, and one source cannot disagree with itself.
artifact_hash() {
  awk -v name="$1" '$2 == name || $2 == "*" name { print $1 }' "$checksum_file"
}

for arch in amd64 arm64; do
  archive="${dist_dir}/driga_${release_version}_darwin_${arch}.tar.gz"
  python3 "$(dirname "$0")/verify-macos-app.py" "$archive" "$release_version"
  if [ "${DRIGA_VERIFY_SIGNATURES:-false}" = true ]; then
    app_temp=$(mktemp -d)
    trap 'rm -rf "$app_temp"' EXIT
    tar -xzf "$archive" -C "$app_temp" Driga.app
    codesign --verify --deep --strict --verbose=2 "$app_temp/Driga.app"
    xcrun stapler validate "$app_temp/Driga.app"
    spctl --assess --type execute --verbose=2 "$app_temp/Driga.app"
    rm -rf "$app_temp"
    trap - EXIT
  fi
done
for platform in linux_amd64 linux_arm64; do
  archive="${dist_dir}/driga_${release_version}_${platform}.tar.gz"
  entries=$(tar -tzf "$archive" | LC_ALL=C sort)
  if [ "$entries" != $'LICENSE\nNOTICE\nREADME.md\ndriga\nthird-party/fonts-LICENSE.txt\nthird-party/vendor-LICENSES.txt' ]; then
    echo "unexpected contents in $(basename "$archive"):" >&2
    printf '%s\n' "$entries" >&2
    exit 1
  fi
done
for platform in windows_amd64 windows_arm64; do
  archive="${dist_dir}/driga_${release_version}_${platform}.zip"
  entries=$(unzip -Z1 "$archive" | LC_ALL=C sort)
  if [ "$entries" != $'LICENSE\nNOTICE\nREADME.md\ndriga.exe\nthird-party/fonts-LICENSE.txt\nthird-party/vendor-LICENSES.txt' ]; then
    echo "unexpected contents in $(basename "$archive"):" >&2
    printf '%s\n' "$entries" >&2
    exit 1
  fi
done

# What the contract promises about a package is where it puts the binary, which
# is the one thing nfpm's own output format cannot guarantee on its behalf.
if command -v dpkg-deb >/dev/null 2>&1; then
  for arch in amd64 arm64; do
    deb="${dist_dir}/driga_${release_version}_linux_${arch}.deb"
    if ! dpkg-deb -c "$deb" | grep -q ' \./usr/bin/driga$'; then
      echo "Debian package does not install /usr/bin/driga: $(basename "$deb")" >&2
      exit 1
    fi
  done
fi

scoop_manifest="${dist_dir}/scoop/driga.json"
if [ ! -f "$scoop_manifest" ]; then
  echo "Scoop manifest is missing: ${scoop_manifest}" >&2
  exit 1
fi
if [ "$(jq -r '.version' "$scoop_manifest")" != "$release_version" ]; then
  echo "Scoop manifest version does not match ${release_version}." >&2
  exit 1
fi
for architecture in 64bit:amd64 arm64:arm64; do
  scoop_arch=${architecture%%:*}
  artifact_arch=${architecture##*:}
  expected_url="https://github.com/labstack/driga/releases/download/${release_tag}/driga_${release_version}_windows_${artifact_arch}.zip"
  actual_url=$(jq -r --arg arch "$scoop_arch" '.architecture[$arch].url' "$scoop_manifest")
  if [ "$actual_url" != "$expected_url" ]; then
    echo "Scoop ${scoop_arch} URL = ${actual_url}, want ${expected_url}." >&2
    exit 1
  fi
  if ! jq -e --arg arch "$scoop_arch" '.architecture[$arch].bin == ["driga.exe"]' "$scoop_manifest" >/dev/null; then
    echo "Scoop ${scoop_arch} entry does not install driga.exe." >&2
    exit 1
  fi
  expected_hash=$(artifact_hash "driga_${release_version}_windows_${artifact_arch}.zip")
  actual_hash=$(jq -r --arg arch "$scoop_arch" '.architecture[$arch].hash' "$scoop_manifest")
  if [ "$actual_hash" != "$expected_hash" ]; then
    echo "Scoop ${scoop_arch} hash does not match its Windows archive." >&2
    exit 1
  fi
done

cask="${dist_dir}/homebrew/Casks/driga.rb"
if [ ! -f "$cask" ]; then
  echo "Homebrew Cask is missing: ${cask}" >&2
  exit 1
fi
if ! grep -Fq 'cask "driga" do' "$cask" || ! grep -Fq 'binary "#{caskroom_path}/Driga.app/Contents/MacOS/driga"' "$cask"; then
  echo "Homebrew Cask does not install driga." >&2
  exit 1
fi
if ! grep -Fq 'app "Driga.app", target: "#{caskroom_path}/Driga.app"' "$cask"; then
  echo "Homebrew Cask does not keep the app at a stable Caskroom path." >&2
  exit 1
fi
if ! grep -Fqx "  version \"${release_version}\"" "$cask"; then
  echo "Homebrew Cask version does not match ${release_version}." >&2
  exit 1
fi
if grep -Eq '^[[:space:]]+verified:' "$cask"; then
  echo "Homebrew Cask contains deprecated URL verification metadata." >&2
  exit 1
fi
# The cask holds a url and sha256 for macOS on Intel and ARM. A
# file-wide grep therefore still passes when two of them are swapped, which is a
# cask that hands every user the wrong digest — so read each block on its own.
for pair in intel:amd64 arm:arm64; do
  cask_arch=${pair%%:*}
  artifact_arch=${pair##*:}
  block=$(awk '/on_macos do/,/^  end$/' "$cask" | awk "/on_${cask_arch} do/,/end/")
  # GoReleaser writes Ruby interpolation into a release Cask (`#{version}`),
  # while snapshots may carry a literal version. Resolve the one supported
  # interpolation only after binding the Cask's version above, then compare the
  # complete URL so neither form can hide the wrong release or repository.
  url=$(sed -n 's/^ *url "\([^"]*\)".*/\1/p' <<< "$block" | head -n 1)
  url=${url//'#{version}'/$release_version}
  expected_url="https://github.com/labstack/driga/releases/download/${release_tag}/driga_${release_version}_darwin_${artifact_arch}.tar.gz"
  if [ "$url" != "$expected_url" ]; then
    echo "Homebrew Cask ${artifact_arch} URL = ${url}, want ${expected_url}." >&2
    exit 1
  fi
  if ! grep -Fq "$(artifact_hash "driga_${release_version}_darwin_${artifact_arch}.tar.gz")" <<< "$block"; then
    echo "Homebrew Cask ${artifact_arch} hash does not match its macOS archive." >&2
    exit 1
  fi
done

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) native_binary="${dist_dir}/driga_darwin_arm64_v8.0/Driga.app/Contents/MacOS/driga" ;;
  Darwin-x86_64) native_binary="${dist_dir}/driga_darwin_amd64_v1/Driga.app/Contents/MacOS/driga" ;;
  Linux-aarch64 | Linux-arm64) native_binary="${dist_dir}/driga_linux_arm64_v8.0/driga" ;;
  Linux-x86_64 | Linux-amd64) native_binary="${dist_dir}/driga_linux_amd64_v1/driga" ;;
  *)
    echo "unsupported verification host: $(uname -s)-$(uname -m)" >&2
    exit 1
    ;;
esac
if [ -n "$native_binary" ]; then
  version_output=$("$native_binary" version --json)
  # Snapshots have a synthetic version; real release versions are exactly the
  # tag without its leading v, preserving Driga's existing version contract.
  expected_version=${release_tag#v}
  if [[ "$release_version" =~ -SNAPSHOT-[0-9a-f]{7,40}$ ]]; then
    expected_version=$release_version
  fi
  if ! jq -e --arg version "$expected_version" \
    '.schema == 2 and .data.version == $version' <<< "$version_output" >/dev/null; then
    echo "native binary does not report release tag ${release_tag} (version ${expected_version})." >&2
    exit 1
  fi
  if ! jq -e '.data.commit | test("^[0-9a-f]{40}$")' <<< "$version_output" >/dev/null ||
    ! jq -e '.data.date | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T")' <<< "$version_output" >/dev/null; then
    echo "native binary has no embedded commit or date." >&2
    exit 1
  fi
fi

# Inspect the archived PE resources without executing either Windows binary.
absolute_dist=$(cd "$dist_dir" && pwd)
repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
(cd "$repository_root" && go test ./scripts/windows-resources -run '^TestReleaseResources$' -count=1 -args -dist "$absolute_dist" -version "$release_version")

echo "verified 6 archives, 4 Linux packages, ${distributable_count} checksums, Homebrew, Scoop, Windows PE resources, and embedded build metadata for ${release_tag}"
