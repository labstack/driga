#!/usr/bin/env bash
# Local packaging only. Never creates tags, releases, or network installs.
set -euo pipefail

die() {
	printf 'release: %s\n' "$*" >&2
	exit 1
}
dry_run=false
if [[ ${1:-} == --dry-run ]]; then
	dry_run=true
	shift
fi
[[ $# == 1 ]] || die 'usage: scripts/release.sh [--dry-run] vYYYY.M.REVISION[-alpha]'
tag=$1
# Calendar versions: unpadded month, revision zero at each UTC month boundary.
python3 - "$tag" <<'PYVERSION'
import re, sys
pattern = r'^v[1-9][0-9]{3}\.([1-9]|1[0-2])\.(0|[1-9][0-9]{0,18})(-alpha)?$'
if not re.fullmatch(pattern, sys.argv[1]):
    sys.exit('release: expected vYYYY.M.REVISION[-alpha] (month 1–12, no leading zeros)')
PYVERSION

root=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
cd "$root"
commit=$(git rev-parse HEAD)
dirty=$(git status --porcelain --untracked-files=all)
if [[ $dry_run == false ]]; then
	[[ -z $dirty ]] || die 'refusing dirty tree (including untracked files); use --dry-run for local testing'
	[[ $(git rev-parse --verify "refs/tags/$tag^{commit}") == "$commit" ]] || die 'tag must exist and point at HEAD'
	[[ -s LICENSE ]] || die 'missing top-level LICENSE; maintainer must choose one before distribution'
	[[ ! -e dist ]] || die 'dist already exists; retain previous artifacts elsewhere before building'
	output=$root/dist
	mkdir "$output"
else
	output=$(mktemp -d "${TMPDIR:-/tmp}/driga-release-dry-run.XXXXXX")
	printf 'DRY RUN — NON-DISTRIBUTABLE; dirty tree, tag and license gates bypassed.\n' >&2
	[[ -z $dirty ]] || commit=$commit-dirty
fi
work=$(mktemp -d "${TMPDIR:-/tmp}/driga-release-work.XXXXXX")
trap 'rm -rf "$work"' EXIT

export LC_ALL=C TZ=UTC
export CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOFLAGS='' GOEXPERIMENT=''
export GOAMD64=v1 GOARM64=v8.0
required_go=$(awk '$1 == "go" {print $2}' go.mod)
[[ $(go env GOVERSION) == "go$required_go" ]] || die "requires Go $required_go (go.mod), without automatic toolchain installation"
epoch=$(git show -s --format=%ct HEAD)
build_date=$(
	python3 - "$epoch" <<'PY'
from datetime import datetime, timezone
import sys
print(datetime.fromtimestamp(int(sys.argv[1]), timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'))
PY
)
version=${tag#v}
ldflags="-s -w -X main.version=$version -X main.commit=$commit -X main.date=$build_date"
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
	target_os=${target%/*}
	target_arch=${target#*/}
	suffix=''
	[[ $target_os != windows ]] || suffix=.exe
	printf 'Building %s (%s)\n' "$target" "$version" >&2
	GOOS=$target_os GOARCH=$target_arch go build -mod=readonly -trimpath -buildvcs=false \
		-ldflags "$ldflags" -o "$work/driga_${target_os}_${target_arch}${suffix}" ./cmd/driga
done

if [[ $dry_run == false ]]; then
	[[ $(git rev-parse HEAD) == "$commit" && -z $(git status --porcelain --untracked-files=all) ]] || die 'source changed during the build; refusing to package'
	[[ $(git rev-parse --verify "refs/tags/$tag^{commit}") == "$commit" ]] || die 'release tag changed during the build'
fi

# Python's standard library avoids GNU/BSD tar differences. Normalize every
# header, file mode, owner, timestamp, member order, and the gzip filename/mtime.
python3 - "$work" "$output" "$version" "$commit" "$build_date" "$epoch" "$dry_run" "$ldflags" <<'PY'
import gzip, io, json, pathlib, subprocess, sys, tarfile, zipfile
work, output = map(pathlib.Path, sys.argv[1:3])
version, commit, date, epoch, dry_run, ldflags = sys.argv[3:]
targets = ['darwin/arm64', 'darwin/amd64', 'linux/amd64', 'linux/arm64', 'windows/amd64', 'windows/arm64']
for target in targets:
    os_name, arch = target.split('/')
    binary = 'driga.exe' if os_name == 'windows' else 'driga'
    suffix = '.exe' if os_name == 'windows' else ''
    files = {
        binary: (work / f'driga_{os_name}_{arch}{suffix}').read_bytes(),
        'README.md': pathlib.Path('README.md').read_bytes(),
        'third-party/vendor-LICENSES.txt': pathlib.Path('internal/web/static/vendor/LICENSES.txt').read_bytes(),
        'third-party/fonts-LICENSE.txt': pathlib.Path('internal/web/static/fonts/LICENSE.txt').read_bytes(),
    }
    if pathlib.Path('LICENSE').is_file():
        files['LICENSE'] = pathlib.Path('LICENSE').read_bytes()
    if pathlib.Path('NOTICE').is_file():
        files['NOTICE'] = pathlib.Path('NOTICE').read_bytes()
    if dry_run == 'true':
        files['NON-DISTRIBUTABLE.txt'] = b'DRY RUN ONLY. Unreviewed source; tag and license gates bypassed. Do not distribute.\n'
    if os_name == 'windows':
        from datetime import datetime, timezone
        # ZIP uses UTC DOS timestamps (two-second resolution, >= 1980).
        stamp = datetime.fromtimestamp(max(315532800, int(epoch)), timezone.utc)
        timestamp = (stamp.year, stamp.month, stamp.day, stamp.hour, stamp.minute, stamp.second // 2 * 2)
        archive = output / f'driga_{version}_{os_name}_{arch}.zip'
        with zipfile.ZipFile(archive, 'x', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zip:
            for name, data in sorted(files.items()):
                info = zipfile.ZipInfo(name, timestamp)
                info.create_system = 3
                info.external_attr = (0o100755 if name == binary else 0o100644) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                zip.writestr(info, data, compresslevel=9)
    else:
        archive = output / f'driga_{version}_{os_name}_{arch}.tar.gz'
        with archive.open('xb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0, compresslevel=9) as gz:
            with tarfile.open(fileobj=gz, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                for name, data in sorted(files.items()):
                    info = tarfile.TarInfo(name)
                    info.size, info.mtime = len(data), int(epoch)
                    info.mode = 0o755 if name == binary else 0o644
                    info.uid = info.gid = 0
                    info.uname = info.gname = ''
                    tar.addfile(info, io.BytesIO(data))

record = {
    'schema': 1, 'version': version, 'commit': commit, 'date': date,
    'go_version': subprocess.check_output(['go', 'env', 'GOVERSION'], text=True).strip(),
    'source': 'https://github.com/labstack/driga', 'dry_run': dry_run == 'true',
    'targets': targets, 'compile_only': [],
    'build': {'CGO_ENABLED': '0', 'GOAMD64': 'v1', 'GOARM64': 'v8.0',
              'flags': ['-mod=readonly', '-trimpath', '-buildvcs=false', '-ldflags', ldflags]},
}
(output / 'build-info.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
PY

(
	cd "$output"
	shasum -a 256 driga_*.tar.gz driga_*.zip build-info.json >SHA256SUMS
	shasum -a 256 -c SHA256SUMS
)
printf 'DIST_DIR=%s\n' "$output"
