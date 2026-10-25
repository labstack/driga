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
    # drigad, the Driga service, is the same program under its own name.
    ln -f bin/driga bin/drigad
    echo "built bin/driga and bin/drigad (${version})"

# Put the built binary on PATH, deliberately; it may shadow an installed release.
install: build
    #!/usr/bin/env bash
    set -euo pipefail
    dir="${DRIGA_INSTALL_DIR:-${HOME}/.local/bin}"
    mkdir -p "$dir"
    install -m 0755 bin/driga "$dir/driga"
    ln -f "$dir/driga" "$dir/drigad"
    echo "installed $dir/driga ($("$dir/driga" version | head -1)); first on PATH: $(command -v driga)"

# Remove the checkout build (never the copy on PATH).
clean:
    rm -rf bin dist

# ---------- Verification ----------

# Local pre-commit gate.
check: _fmt-check tidy-check vet generate-check docs-check ui-check test
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

# Rewrite the generated reference pages under site/ from the code.
docs:
    go run ./cmd/driga-docgen

# Fail when a committed reference page is behind the code it documents.
docs-check:
    go run ./cmd/driga-docgen --check

# ---------- App UI ----------

# Install the UI's dependencies from the lockfile.
ui-deps:
    cd ui && bun install --frozen-lockfile

# Develop the app's UI in a browser at http://127.0.0.1:5173/app/: Vite
# hot-reloads the UI and sends the API to driga dev serve, which restarts when
# Go files change (with watchexec on PATH). It reads the data directory the
# environment names, as every driga command does.
dev: ui-deps
    #!/usr/bin/env bash
    set -euo pipefail
    trap 'kill 0' EXIT
    if command -v watchexec >/dev/null; then
      watchexec --restart --exts go --watch cmd --watch internal -- go run ./cmd/driga dev serve &
    else
      go run ./cmd/driga dev serve &
    fi
    cd ui && bun --bun vite

# Typecheck and build the app's dashboard host and the panels MCP App into
# internal/web/app and internal/mcp/apps. Both are committed, so a Go build
# needs no JavaScript toolchain; rebuild after changing ui/.
ui: ui-deps
    cd ui && bun run build

# Test and rebuild the UI, and fail when the build in git (staged counts) is
# behind ui/.
ui-check: ui
    #!/usr/bin/env bash
    set -euo pipefail
    (cd ui && bun run test)
    built=(internal/web/app internal/mcp/apps)
    stale="$(git diff --name-only -- "${built[@]}"; git ls-files --others --exclude-standard -- "${built[@]}")"
    if [[ -n "$stale" ]]; then
      echo "$stale" >&2
      echo "The UI build in git is behind ui/; add the rebuilt files above." >&2
      exit 1
    fi

# ---------- Documentation site ----------

# Install the site's dependencies from the lockfile.
site-deps:
    cd site && npm ci --no-audit --no-fund

# Serve the site locally with live reload on http://localhost:7431.
site: docs site-deps
    cd site && npm run dev

# Re-record site footage of the real TUI on demo data (needs vhs, ffmpeg).
site-footage:
    site/footage/record.sh

# Publish the rendered footage to R2 (assets.driga.dev) and update the manifest.
site-footage-publish:
    cd site/footage && node upload.mjs

# Build the site into site/dist: astro check, build, then the output checks.
# Depends on the checking recipe, not the writing one, so it cannot hide a stale page.
site-build: docs-check site-deps
    cd site && npm run build

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
