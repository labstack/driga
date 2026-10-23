# One set of recipes for local development and CI: `just check` before a
# commit, `just ci` is what the Linux CI job runs.

# Build the CLI into the checkout.
default: build

# A checkout build never shadows an installed release by accident.
# Build driga into ./bin, which is not on PATH.
build:
    #!/usr/bin/env bash
    set -euo pipefail
    # --long keeps the commit suffix on a tagged commit, so a checkout build
    # never passes for a release.
    version=$(git describe --tags --always --dirty --long 2>/dev/null || echo dev)
    mkdir -p bin
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${version} -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o bin/driga ./cmd/driga
    echo "built bin/driga (${version})"

# Put the built binary on PATH, deliberately; it may shadow an installed release.
install: build
    #!/usr/bin/env bash
    set -euo pipefail
    dir="${DRIGA_INSTALL_DIR:-${HOME}/.local/bin}"
    mkdir -p "$dir"
    install -m 0755 bin/driga "$dir/driga"
    echo "installed $dir/driga ($("$dir/driga" version | head -1)); first on PATH: $(command -v driga)"

# Remove the checkout build (never the copy on PATH).
clean:
    rm -rf bin dist

# ---------- Verification ----------

# Local pre-commit gate.
check: _fmt-check tidy-check vet generate-check test
    @echo "All checks passed"

# Adds race, vulnerability, workflow and release-snapshot checks.
# What the Linux CI job runs.
ci: check race vuln workflow-check snapshot
    @echo "CI checks passed"

# Format all Go packages.
fmt:
    gofmt -w .

# gofmt prints nothing both for a clean tree and for a file it cannot parse,
# so its exit status is checked separately from its output.
[private]
_fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! unformatted="$(gofmt -l .)"; then
      echo "gofmt could not read the tree (see the errors above)" >&2
      exit 1
    fi
    if [[ -n "$unformatted" ]]; then
      echo "Unformatted files:" >&2
      echo "$unformatted" >&2
      exit 1
    fi

# Fail when go.mod or go.sum is not tidy.
tidy-check:
    go mod tidy -diff

# Vet natively and for Windows, which has its own build-tagged files.
vet:
    go vet ./...
    GOOS=windows go vet ./...

# Fail when the committed .env.example is behind the settings table.
generate-check:
    go generate ./internal/config
    git diff --exit-code -- .env.example

# Run tests with a disposable home and Driga data (scripts/isolated.sh).
test *args:
    scripts/isolated.sh go test {{ args }} ./...

# Some tests change their budgets under -race (race_on/race_off).
# Run the suite under the race detector.
race:
    CGO_ENABLED=1 scripts/isolated.sh go test -race ./...

# Run the suite in UTC, as CI runners do.
test-utc:
    TZ=UTC scripts/isolated.sh go test -count=1 ./...

# Windows end-to-end suite (run on Windows; CI runs it as its own job).
e2e:
    scripts/isolated.sh go test -tags e2e ./internal/e2e -v -timeout 25m

# Scan reachable code against the Go vulnerability database.
vuln:
    govulncheck ./...

# actionlint silently skips the shell in run: blocks without shellcheck.
# Lint GitHub workflows, including their shell.
workflow-check:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v shellcheck >/dev/null || { echo "shellcheck is not installed; actionlint would skip every run: block" >&2; exit 1; }
    actionlint

# Build every release target without publishing, signing or notarizing.
snapshot:
    goreleaser check
    goreleaser release --snapshot --clean --skip=publish,sign,notarize
    scripts/verify-release-dist.sh dist

# Create and publish the next vYYYY.M.REVISION-alpha tag from releasable main.
release:
    scripts/release.sh
