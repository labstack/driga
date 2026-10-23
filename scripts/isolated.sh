#!/usr/bin/env bash
# Run a command with a private, disposable home: tests must never see the
# real agent configuration (~/.claude, ~/.codex, ...) or Driga's data.
# The Go caches stay where they are; they hold no user state.
set -euo pipefail

gocache=$(go env GOCACHE)
gomodcache=$(go env GOMODCACHE)
root=$(mktemp -d "${TMPDIR:-/tmp}/driga-test.XXXXXX")
trap 'chmod -R u+w "$root" 2>/dev/null; rm -rf "$root"' EXIT
mkdir "$root/home" "$root/data" "$root/spool" # inside the private mktemp dir
: > "$root/config.toml"

unset CLAUDE_CONFIG_DIR COPILOT_HOME
export HOME="$root/home"
export CODEX_HOME="$HOME/.codex" GEMINI_CLI_HOME="$HOME/.gemini"
export XDG_CONFIG_HOME="$HOME/.config" XDG_DATA_HOME="$HOME/.local/share" XDG_STATE_HOME="$HOME/.local/state"
export DRIGA_DATA_DIR="$root/data" DRIGA_SPOOL_DIR="$root/spool" DRIGA_CONFIG="$root/config.toml"
export GOCACHE="$gocache" GOMODCACHE="$gomodcache"

"$@"
