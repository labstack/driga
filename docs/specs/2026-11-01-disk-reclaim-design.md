# Disk reclaim: build and cache cleanup

Status: draft for review. Reviewed by Fable (2026-11-01).

## Problem

`driga cleanup` today reclaims whole worktrees, Claude scratch directories and
reports agent history. On the maintainer's Mac that covered little of the real
usage:

- 27 worktrees held 24.4 GiB and every one was refused (unmerged, tracked
  changes, idle threshold, locked, live session). Most of their size is
  regenerable build output, not source.
- Across `~/Projects` (127 GiB), `du` counted `node_modules` 28.2 GiB, Rust
  `target/` 24.4 GiB, Python venvs 9.6 GiB, `.cache` 10.2 GiB.
- Global tool caches were larger still: `~/.cache/uv` 33 GiB, Go build cache
  35 GiB (cleared to 69 MiB, back to 2.3 GiB within a day), GOMODCACHE 9.9 GiB,
  rustup toolchains 4 GiB, bun 4.1 GiB, npm 3.1 GiB, Homebrew 3 GiB.

Two new candidate kinds close the gap: `build` and `cache`.

## Kind `build`: regenerable directories in a checkout

### Discovery

- Every repo and worktree in the store, including main worktrees and worktrees
  refused as a whole.
- Sibling repositories: directories containing `.git` within depth 2 of each
  known repo's parent directory. On the test machine this adds 57 repos and
  13.2 GiB the store does not know.
- Non-git directories are out of scope.

### Qualification (all must hold)

1. Git reports the directory ignored as a whole: a `!!` entry ending in `/`
   from `git status --porcelain=v1 -z --ignored=matching`. A force-added file
   inside makes git list files instead, so tracked content is never a
   candidate. Nested monorepo paths (`pkg/node_modules/`) are reported the same
   way.
2. Its name matches a built-in rule, and the rule's sibling evidence is
   present. Built-in rules live in a Go table:

   | Directory | Sibling evidence |
   |---|---|
   | `node_modules/` | `package.json` |
   | `target/` | `Cargo.toml` |
   | `.venv/`, `venv/` | `pyproject.toml`, `requirements*.txt`, `uv.lock` or `setup.py` |
   | `.gradle/`, `build/` | `build.gradle*` or `settings.gradle*` |
   | `.next/`, `.nuxt/`, `.svelte-kit/`, `.turbo/` | `package.json` |
   | `dist/` | `package.json` |
   | `.build/` | `Package.swift` |
   | `Pods/` | `Podfile` |
   | `.pio/` | `platformio.ini` |

   User entries in `cleanup.regenerable` are name-only: the user asserts them.

   `__pycache__/` is omitted from standalone build candidates because these
   directories are small and numerous. Worktree removal still treats them as
   regenerable without sibling evidence.
3. Not a symlink (`Lstat`), and contains no `.git` entry at any depth (bounded
   walk). A nested clone inside an ignored directory is never removed.
4. Idle: no process cwd and no open file under the directory itself, and no
   live session in the checkout. A terminal parked in the repo root does not
   block it, unlike whole-worktree removal.
5. Checkout untouched for `cleanup.build_idle` (default 7 days): no commit and
   no session activity in that window.

### Excluded on purpose

- `out/`: rendered media sits beside `package.json` in several repos.
- `.terraform/`: removal resets the selected workspace, so the next `apply` can
  hit the wrong one.
- `.codegraph/` and other tool indexes: not regenerable by the project's own
  build. They can be added by the user through `cleanup.regenerable`.

### Removal

Delete, do not trash. The kind exists because the contents are regenerable,
and a 16 GiB rename into the Trash frees nothing and fails across volumes.
Removal uses `os.Root(checkout).RemoveAll(rel)`, which is fd-relative and does
not follow symlinks. Before removing a venv, its top-level `*.dist-info`
directory basenames are durably written to `cleanup-reconstruction.log` in the
private data directory as a `prepared` record. This package/version list helps
rebuild an ad hoc environment; it is not a complete `pip freeze`. Records are
limited to 64 KiB, and the log retains 10 MiB plus one rotated file. Failure to
write or sync the complete record prevents removal. `cleanup.log` still records
the final outcome, including the package list. Worktree and scratch keep moving
to the Trash.

Open-file checks use one `lsof -Fn` table matched by path prefix, not
`lsof +D`, which walks every file of a large `target/`.

## Kind `cache`: global tool caches

A table of caches. Driga asks tools for their cache locations and runs their
cleanup commands. Bun and Xcode use fixed paths and direct directory removal
through `os.Root`; neither invokes a tool.

| Tool | Location | Command |
|---|---|---|
| Go build | `go env GOCACHE` | `go clean -cache` |
| Stray Go build | direct real directories under temp roots, identified by Go's `README` marker | delete through `os.Root` after over 24h idle |
| Go modules | `go env GOMODCACHE` | `go clean -modcache` (report only by default) |
| uv | `uv cache dir` | `uv cache prune` |
| npm | `npm config get cache` | `npm cache clean --force` |
| pnpm | `pnpm store path` | `pnpm store prune` |
| bun | `BUN_INSTALL_CACHE_DIR`, else `$BUN_INSTALL/install/cache` (`BUN_INSTALL` defaults to `~/.bun`) | delete through `os.Root` (installed `node_modules` are APFS clones/copies) |
| Cargo | `~/.cargo/registry` | report only |
| rustup | `rustup toolchain list` | report toolchains not used by any known repo |
| Homebrew | `brew --cache` | `brew cleanup` |
| Xcode | `~/Library/Developer/Xcode/DerivedData` | delete (Xcode-owned, regenerable) |

Rules:

- Temp Go caches require a regular `README` starting with `This directory holds cached build artifacts from the Go build system.`; deduplicate canonical paths and exclude canonical `go env GOCACHE` (lookup failure excludes nothing extra). The directory and immediate children must be idle over 24h, with no process cwd or open file inside. Discovery and removal are independent of `RunTools`; removal freshly verifies the fingerprint, idle time, liveness, and identity, with refusals visible via `--why`.
- With `RunTools` disabled, the configured-cache exclusion uses only an absolute
  exported `GOCACHE`, resolving symlinks without launching Go. Settings written by
  `go env -w` are not resolved in this mode.
- A tool that is missing or prints no location is skipped.
- Commands run only from the CLI in the user's shell environment, never from
  the launchd service, whose `PATH` and version-manager shims differ.

## Report-only buckets

Shown with size and age so the user can see where space goes. Driga never
removes these: Hugging Face and other model caches, Docker or Colima VM disks
(pruning inside the VM frees no host space without trim), agent history
(`~/.claude/projects`, `~/.codex`, existing `history` kind).

## Sizes

Previews say "up to N", because `du` overcounts APFS clones and hard links
(bun installs `node_modules` as clones of its cache). After removal, Driga
reports the bytes actually freed as the `statfs` free-space delta.

## Surfaces

- `driga cleanup list --kind build|cache`, `driga cleanup remove`, same
  confirmation and `--yes` rules as today.
- The low-disk alert names the largest `build` and `cache` candidates.
- No scheduled automatic cleanup in this change.

## Not in scope

Non-git directories, scheduled cleanup, a user-editable rules file, APFS-exact
size previews.

## Testing

- Fixture repos per rule: qualifying directory, missing sibling evidence,
  force-added file inside, symlinked directory, nested `.git`, `out/` and
  `.terraform/` never listed.
- Liveness: process cwd inside the directory refuses; cwd in the repo root
  does not.
- `cache`: fake tool binaries on `PATH` for present, missing and empty-output
  cases; the service path never runs a command.
- End-to-end on the built binary against a scratch tree, verifying the
  `statfs` delta and `cleanup.log` entries.
