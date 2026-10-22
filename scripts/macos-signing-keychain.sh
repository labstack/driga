#!/usr/bin/env bash
# Release runner only. Never enable shell tracing: inputs contain signing keys.
set -euo pipefail
umask 077
: "${RUNNER_TEMP:?GitHub-hosted runner required}"
: "${GITHUB_ENV:?GitHub Actions environment required}"
: "${MACOS_SIGN_P12:?}"
: "${MACOS_SIGN_PASSWORD:?}"
: "${MACOS_NOTARY_KEY:?}"
: "${MACOS_NOTARY_KEY_ID:?}"
: "${MACOS_NOTARY_ISSUER_ID:?}"

signing_dir="$RUNNER_TEMP/driga-signing"
mkdir -m 700 "$signing_dir"
keychain="$signing_dir/signing.keychain-db"
password=$(openssl rand -hex 32)
# The workflow's always() cleanup also covers failures during this setup.
# notary.p8 stays for the build hook; the workflow's cleanup removes the directory.
trap 'rm -f "$signing_dir/certificate.p12"' EXIT
printf '%s' "$MACOS_SIGN_P12" | base64 --decode > "$signing_dir/certificate.p12"
printf '%s' "$MACOS_NOTARY_KEY" | base64 --decode > "$signing_dir/notary.p8"
security create-keychain -p "$password" "$keychain"
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$password" "$keychain"
security import "$signing_dir/certificate.p12" -k "$keychain" -P "$MACOS_SIGN_PASSWORD" -T /usr/bin/codesign >/dev/null
security set-key-partition-list -S apple-tool:,apple:,codesign: -k "$password" "$keychain" >/dev/null
# Do not accept an Installer identity or silently choose between certificates.
identities=$(security find-identity -v -p codesigning "$keychain")
identity=$(sed -n 's/.* \([0-9A-F]\{40\}\) "Developer ID Application:.*"/\1/p' <<< "$identities")
if [ "$(printf '%s\n' "$identity" | sed '/^$/d' | wc -l | tr -d ' ')" != 1 ]; then
  echo 'Expected exactly one Developer ID Application signing identity.' >&2
  exit 1
fi
{
  echo "DRIGA_SIGNING_KEYCHAIN=$keychain"
  echo "DRIGA_SIGNING_IDENTITY=$identity"
  echo "DRIGA_NOTARY_KEY=$signing_dir/notary.p8"
  echo "DRIGA_NOTARY_KEY_ID=$MACOS_NOTARY_KEY_ID"
  echo "DRIGA_NOTARY_ISSUER=$MACOS_NOTARY_ISSUER_ID"
} >> "$GITHUB_ENV"
