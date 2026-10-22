#!/usr/bin/env bash
set -euo pipefail

binary=${1:?binary path required}
goos=${2:?OS required}
version=${3:?version required}
snapshot=${4:?snapshot flag required}
[ "$goos" = darwin ] || exit 0

contents=$(dirname "$(dirname "$binary")")
app=$(dirname "$contents")
build_dir=$(dirname "$app")
python3 scripts/macos-app.py "$app" "$version"

if [ "$snapshot" != true ]; then
  [ "$(uname -s)" = Darwin ] || { echo 'App signing requires macOS.' >&2; exit 1; }
  : "${DRIGA_SIGNING_KEYCHAIN:?temporary signing keychain required}"
  : "${DRIGA_SIGNING_IDENTITY:?Developer ID Application identity required}"
  # Signing the outer bundle also signs its main executable and seals resources.
  codesign --force --sign "$DRIGA_SIGNING_IDENTITY" --keychain "$DRIGA_SIGNING_KEYCHAIN" \
    --options runtime --timestamp "$app"
  codesign --verify --deep --strict --verbose=2 "$app"
  submission="$build_dir/notary-submission.zip"
  ditto -c -k --keepParent "$app" "$submission"
  xcrun notarytool submit "$submission" --keychain "$DRIGA_SIGNING_KEYCHAIN" \
    --keychain-profile driga --wait --timeout 20m --output-format json > "$build_dir/notary-result.json"
  submission_id=$(jq -er '.id // empty' "$build_dir/notary-result.json")
  xcrun notarytool log "$submission_id" --keychain "$DRIGA_SIGNING_KEYCHAIN" \
    --keychain-profile driga "$build_dir/notary-log.json"
  jq '{status, statusSummary, issues}' "$build_dir/notary-log.json"
  jq -e '.status == "Accepted"' "$build_dir/notary-result.json" >/dev/null
  jq -e '[.issues[]? | select(.severity == "error")] | length == 0' "$build_dir/notary-log.json" >/dev/null
  xcrun stapler staple "$app"
  xcrun stapler validate "$app"
  codesign --verify --deep --strict --verbose=2 "$app"
  spctl --assess --type execute --verbose=2 "$app"
  rm "$submission"
fi

# OSS archives add the build binary themselves. Stage the remaining complete
# bundle (including the staple) outside the sealed app, preserving its layout.
resources="$build_dir/bundle-resources"
mkdir -p "$resources"
cp -R "$app/Contents" "$resources/Contents"
rm "$resources/Contents/MacOS/driga"
