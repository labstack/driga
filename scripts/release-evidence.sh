#!/usr/bin/env bash
# Optional local supply-chain evidence. No installs, publication, or log uploads.
set -euo pipefail
die() {
	printf 'release-evidence: %s\n' "$*" >&2
	exit 1
}
[[ $# == 2 || $# == 3 ]] || die 'usage: release-evidence.sh sbom|sign DIST [MANAGED_KEY_PATH]'
mode=$1
output=$(cd "$2" && pwd)
cd "$output"
[[ -f build-info.json && -f SHA256SUMS ]] || die 'not a release output directory'
shasum -a 256 -c SHA256SUMS
case "$mode" in
sbom)
	[[ $# == 2 ]] || die 'sbom takes only DIST'
	[[ ! -e SHA256SUMS.sig ]] || die 'refusing to change a signed manifest'
	command -v syft >/dev/null || die 'requires preinstalled syft 1.33.0; nothing was installed'
	syft version -o json | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["version"].removeprefix("v") == "1.33.0" else "requires syft 1.33.0")'
	work=$(mktemp -d "${TMPDIR:-/tmp}/driga-sbom.XXXXXX")
	trap 'rm -rf "$work"' EXIT
	for archive in driga_*.tar.gz; do
		sbom=${archive%.tar.gz}.spdx.json
		[[ ! -e $sbom ]] || die "refusing to replace $sbom"
		tar -xzf "$archive" -C "$work" driga
		SYFT_CHECK_FOR_APP_UPDATE=false syft scan "file:$work/driga" -o "spdx-json=$output/$sbom"
	done
	# Include SBOMs in the manifest that is subsequently signed.
	shasum -a 256 driga_*.tar.gz build-info.json driga_*.spdx.json >"$work/SHA256SUMS"
	mv "$work/SHA256SUMS" SHA256SUMS
	shasum -a 256 -c SHA256SUMS
	;;
sign)
	[[ $# == 3 ]] || die 'sign requires a managed key path (never a secret value)'
	python3 -c 'import json,sys; sys.exit("refusing to sign dry-run artifacts" if json.load(open("build-info.json"))["dry_run"] else 0)'
	command -v cosign >/dev/null || die 'requires preinstalled cosign 2.6.0; nothing was installed'
	cosign version --json | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["gitVersion"] == "v2.6.0" else "requires cosign 2.6.0")'
	[[ ! -e SHA256SUMS.sig ]] || die 'refusing to replace SHA256SUMS.sig'
	[[ -f $3 ]] || die 'key must be an absolute path to an existing managed mount'
	[[ $3 == /* ]] || die 'key must be an absolute path'
	# If needed, COSIGN_PASSWORD must be runtime-injected; never echo or persist it.
	cosign sign-blob --yes --key "$3" --tlog-upload=false \
		--output-signature SHA256SUMS.sig SHA256SUMS
	;;
*) die 'mode must be sbom or sign' ;;
esac
