# Disk Reclaim (build and cache cleanup) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `build` and `cache` cleanup kinds so `driga cleanup` reclaims regenerable build output inside checkouts and global tool caches, not only whole worktrees and scratch.

**Architecture:** Both kinds plug into the existing `internal/cleanup` List → verify → Apply pipeline as new `Kind` values with their own candidate and inspect functions. `build` deletes a directory through an `os.Root` opened on its checkout; `cache` runs the owning tool's own command. Open-file evidence moves from one `lsof +D` per candidate to one open-file table per pass, stored on the snapshot.

**Tech Stack:** Go 1.27 (`os.Root`), git CLI, `lsof` (darwin), `/proc` (linux).

**Spec:** `docs/specs/2026-11-01-disk-reclaim-design.md`

## Global Constraints

- Removal is never automatic; every action re-verifies just before acting, as existing kinds do.
- `build` deletes (`os.Root.RemoveAll`); worktree and scratch keep moving to the Trash.
- `cache` commands run only when `Env.RunTools` is true. Only CLI entry points (`driga cleanup`, `driga top`) set it; the service never does.
- Never removed by any kind: `out/`, `.terraform/`, `.codegraph/`, model caches (`~/.cache/huggingface`), Docker/Colima disks, agent history.
- Non-git directories are out of scope.
- `build` is supported on darwin and linux. On windows `buildCandidates` returns nil, like `scratchCandidates`.
- Every attempt is appended to `cleanup.log` through the existing `Apply` defer.
- One commit at the end of all tasks (maintainer preference); stage by explicit path, never `git add -A`. No push, PR or release.

## Review Focus

1. **Huge `target/` (500k files).** Listing must not walk it for open files; sizing stays opt-in (`--sizes`) with the existing 30 s cap. Test: `TestBuildListDoesNotProbePerCandidate` in Task 3.
2. **Directory replaced by a symlink between list and remove.** Remove must refuse, not follow. Test: `TestBuildSymlinkSwapRefused` in Task 4.
3. **Terminal parked in the repo root.** Must not block a `node_modules` candidate; a cwd inside `node_modules` must. Test: `TestBuildRootCwdDoesNotBlock` in Task 3.
4. **Tool missing from `PATH` or printing nothing.** Cache candidate is skipped, not an error that fails the whole list. Test: `TestCacheMissingToolSkipped` in Task 5.
5. **Repo with no commits (unborn HEAD) or a detached worktree.** Idle check must not error the whole list; that checkout's candidates are refused with a reason. Test: `TestBuildUnbornRepoRefused` in Task 3.

---

### Task 1: Open-file table on the snapshot

**Files:**
- Modify: `internal/platform/platform.go:59-62`, `internal/platform/files_darwin.go`, `internal/platform/files_linux.go`, `internal/platform/files_windows.go`, `internal/platform/fake.go`
- Modify: `internal/cleanup/cleanup.go` (`snapshot`, `read`), `internal/cleanup/scratch.go:167-177`
- Test: `internal/platform/files_unix_test.go` (create, `//go:build !windows`), `internal/cleanup/cleanup_test.go` (fakePlatform)

**Interfaces:**
- Produces: `platform.OpenFileLister` interface `OpenFilePaths(ctx context.Context) ([]string, error)` returning absolute, symlink-resolved paths of every file open by any process the user can inspect. `snapshot.open []string` (nil when the platform cannot list; callers fail closed). `func openUnder(s snapshot, dir string) (bool, error)`.

- [ ] **Step 1: Write failing tests**
  - `TestOpenFilePathsSeesOwnFile`: open a temp file in the test process; `OpenFilePaths` result contains `filepath.EvalSymlinks(path)`.
  - In `cleanup_test.go`, change `fakePlatform` to implement `OpenFilePaths` from a new field `openPaths []string`. `TestOpenUnderPrefix`: `openUnder` is true for `/a/node_modules/x`, false for `/a/node_modules2/x` and `/a/x` with dir `/a/node_modules`; errors when `snapshot.open` is nil.
- [ ] **Step 2: Run** `go test ./internal/platform/ ./internal/cleanup/ -run 'OpenFilePaths|OpenUnder'` — expect compile failure.
- [ ] **Step 3: Implement**
  - darwin: one `lsof -Fn -w` run (no `+D`), keep `n` lines that are absolute paths; exit code 1 with empty output means none.
  - linux: readlink every `/proc/*/fd/*` the user can read; skip `EACCES`/`ENOENT`.
  - windows: return an error ("open-file listing unavailable").
  - `read` fills `s.open` once per pass when the platform implements `OpenFileLister`.
  - Scratch keeps using `FileProber.OpenFiles` (its directories are small); do not change its behaviour.
- [ ] **Step 4: Run** the same command — PASS. Run `go test ./internal/cleanup/ ./internal/platform/` — PASS.

### Task 2: Build rules and qualification

**Files:**
- Create: `internal/cleanup/build_rules.go`
- Test: `internal/cleanup/build_rules_test.go`

**Interfaces:**
- Produces:
  - `type buildRule struct { Dir string; Evidence []string }` where `Evidence` holds glob patterns matched in the directory's parent; empty means no evidence needed.
  - `var buildRules = []buildRule{...}`, exactly the spec table: `node_modules`←`package.json`; `target`←`Cargo.toml`; `.venv`,`venv`←`pyproject.toml`,`requirements*.txt`,`uv.lock`,`setup.py`; `.gradle`,`build`←`build.gradle*`,`settings.gradle*`; `.next`,`.nuxt`,`.svelte-kit`,`.turbo`,`dist`←`package.json`; `.build`←`Package.swift`; `Pods`←`Podfile`; `.pio`←`platformio.ini`.
  - `var neverBuild = []string{"out", ".terraform", ".codegraph"}`.
  - `func qualifyBuild(checkout, rel string, user []string) (rule string, err error)` where `rel` is git's `!!` path ending in `/`. Order: reject if base name is in `neverBuild` (even if the user listed it — user entries cannot override this list), match built-in rule plus at least one evidence glob in `filepath.Dir(filepath.Join(checkout, rel))`, else accept a `user` entry by name (no evidence), else error `"not a known regenerable directory"`. Error text when evidence is missing: `"no <first evidence> beside <dir>"`.
- `cleanup.regenerable` defaults become user-only additions: change the default in `internal/config/settings.go:54` to `[]` and its help text to "Extra regenerable directory names, beyond the built-in rules." Update worktree classification to share the build rules through `qualifyRegenerable`, which also accepts the worktree-only `regenerableOnly` names (`__pycache__`, no evidence). These names are omitted from standalone build candidates.

- [ ] **Step 1: Write failing table test** `TestQualifyBuild` with cases: `node_modules/` + `package.json` → ok; `node_modules/` without it → error `no package.json beside node_modules`; `pkg/node_modules/` + `pkg/package.json` → ok; `out/` + `package.json` → error; `.terraform/` with user list `[".terraform/"]` → error; `.codegraph/` with user list `[".codegraph/"]` → error; `models/` with user list `["models/"]` → ok; `.venv/` + `requirements-dev.txt` → ok; `build/` + `build.gradle.kts` → ok.
- [ ] **Step 2: Run** `go test ./internal/cleanup/ -run TestQualifyBuild` — FAIL.
- [ ] **Step 3: Implement** `qualifyBuild` and the worktree call-site change.
- [ ] **Step 4: Run** `go test ./internal/cleanup/ ./internal/config/` — PASS (fix existing worktree tests whose fixtures relied on the old name-only list by adding the matching evidence file).

### Task 3: Build candidates (discovery, liveness, idle)

**Files:**
- Create: `internal/cleanup/build.go`
- Modify: `internal/cleanup/cleanup.go` (`Kind` const `Build Kind = "build"`, `List`), `internal/config/config.go:56-59`, `internal/config/settings.go`
- Test: `internal/cleanup/build_test.go`

**Interfaces:**
- Consumes: `qualifyBuild`, `openUnder`, `snapshot.open`.
- Produces:
  - `CleanupConfig.BuildIdle time.Duration`, setting `cleanup.build_idle`, default `"168h"`, help "Checkout idle time before its build output is a candidate.", range text as `cleanup.worktree_idle`.
  - `func checkouts(s snapshot) []string` — canonical paths: every stored repo and worktree, plus directories containing `.git` (dir or file) at depth 1 or 2 below each stored repo's parent directory; deduplicated; skips paths under `node_modules`.
  - `func buildCandidates(ctx context.Context, e Env, s snapshot) []Candidate`.
  - `func inspectBuild(ctx context.Context, e Env, s snapshot, checkout, rel string) (Candidate, error)`.
  - Candidate fields: `ID: "build:" + hex(sha256(path))[:12]`, `Kind: Build`, `Path` absolute dir, `Repo: checkout`, `root: checkout`, `Evidence` `["<rule> beside <evidence file>", "checkout idle since <RFC3339>"]`, `Status: "ready"`; `identity` captured via `captureIdentity`.
- `inspectBuild` refusals (exact text, used with `--why`): `"symlink"`, `"contains a nested git repository"`, `"process cwd is inside"`, `"process has an open file inside"`, `"open-file inspection unavailable"`, `"live session in checkout"`, `"checkout active within cleanup.build_idle"`, `"checkout has no commits"`. Idle = latest of `git log -1 --format=%ct` and `LastActivityAt`/`EndedAt` of sessions whose cwd is inside the checkout; must be older than `BuildIdle`. Nested `.git` check is a `WalkDir` that stops at the first `.git` entry and gives up (refuses) after 30 s.
- `List` appends `buildCandidates` after worktrees; with `e.Why` refused build candidates are listed like refused worktrees. `--why` is the only way refused build candidates appear.

- [ ] **Step 1: Write failing tests** (extend `fixture`: add `package.json` and `node_modules/` to `.gitignore` in the repo; set `e.Cfg.BuildIdle = 7*24h`; commit date already 2026-09-01, `Now` 2026-10-18):
  - `TestBuildNodeModulesReady`: `repo/node_modules/x` → candidate `ready`, `Kind == Build`, evidence mentions `package.json`.
  - `TestBuildRootCwdDoesNotBlock`: fake proc cwd = repo root → still `ready`; cwd = `repo/node_modules` → absent without `Why`, `refused` "process cwd is inside" with `Why`.
  - `TestBuildOpenFileBlocks`: `openPaths` contains `repo/node_modules/x` → refused.
  - `TestBuildNestedGitRefused`: `repo/node_modules/dep/.git/HEAD` → refused.
  - `TestBuildRecentCommitRefused`: commit dated `Now - 1h` → refused "checkout active".
  - `TestBuildUnbornRepoRefused`: a stored repo with `git init` only → its candidates refused "checkout has no commits"; `List` returns no error.
  - `TestBuildSiblingDiscovered`: an unstored repo beside the fixture repo, with `node_modules/` + `package.json` → listed.
  - `TestBuildTrackedLookalikeAbsent`: tracked `internal/target/x.go` beside `Cargo.toml` → never listed (git does not report it `!!`).
  - `TestBuildListDoesNotProbePerCandidate`: three candidates → `fakePlatform` `OpenFiles` call count 0 and `OpenFilePaths` call count 1.
- [ ] **Step 2: Run** `go test ./internal/cleanup/ -run TestBuild` — FAIL.
- [ ] **Step 3: Implement** `build.go`, the `Kind`, config field and setting.
- [ ] **Step 4: Run** `go test ./internal/cleanup/ ./internal/config/` — PASS.

### Task 4: Build removal

**Files:**
- Modify: `internal/cleanup/cleanup.go` (`Result`, `verify`, `Apply`)
- Create: `internal/cleanup/statfs_unix.go`, `internal/cleanup/statfs_windows.go`
- Test: `internal/cleanup/build_test.go`

**Interfaces:**
- Produces: `Result.FreedBytes int64 \`json:"freed_bytes,omitempty"\``, `Result.Packages []string \`json:"packages,omitempty"\``. `func freeBytes(path string) (int64, error)` (`unix.Statfs` `Bavail*Bsize`; windows `GetDiskFreeSpaceEx`).
- `verify` gains `case Build:` re-running `inspectBuild` for `c.Repo`, rel `filepath.Rel(c.Repo, c.Path)`.
- `Apply` for `Build`: `r.Commands = [][]string{{"remove", c.Path}}`; dry run returns there. Otherwise: for venv rules (`.venv`, `venv`) record `Packages` as the names of `*.dist-info` dirs under `lib/python*/site-packages`; read `freeBytes(c.Repo)`; `root, _ := os.OpenRoot(c.Repo)`; `root.RemoveAll(rel)`; `FreedBytes = after - before` (floor 0). The `Apply` defer already logs the result, so `Packages` lands in `cleanup.log`.

- [ ] **Step 1: Write failing tests**
  - `TestBuildRemoveDeletes`: list, `Apply` → dir gone, checkout and tracked files intact, `cleanup.log` last entry has `result.commands[0][0] == "remove"`, no Trash entry under `e.Home`.
  - `TestBuildSymlinkSwapRefused`: list, replace `node_modules` with a symlink to an outside dir holding a sentinel file, `Apply` → error, sentinel still present.
  - `TestBuildDryRun`: `DryRun` → dir still present, `Commands` set.
  - `TestBuildVenvPackagesLogged`: `.venv/lib/python3.13/site-packages/requests-2.32.0.dist-info/` → `Result.Packages == ["requests-2.32.0"]`.
- [ ] **Step 2: Run** `go test ./internal/cleanup/ -run 'TestBuild(Remove|Symlink|DryRun|Venv)'` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/cleanup/` — PASS.

### Task 5: Cache kind and report-only buckets

**Files:**
- Create: `internal/cleanup/cache.go`
- Modify: `internal/cleanup/cleanup.go` (`Kind` const `Cache Kind = "cache"`, `Env.RunTools bool`, `List`, `verify`, `Apply`), `internal/cleanup/size.go:75`
- Test: `internal/cleanup/cache_test.go`

**Interfaces:**
- Produces:
  - `type cacheTool struct { ID, Binary string; Locate []string; Clean []string; ReportOnly bool }`.
  - `var cacheTools`, exactly: `go-build` (`go env GOCACHE` / `go clean -cache`); `go-mod` (`go env GOMODCACHE` / report only); `uv` (`uv cache dir` / `uv cache prune`); `npm` (`npm config get cache` / `npm cache clean --force`); `pnpm` (`pnpm store path` / `pnpm store prune`); `bun` (fixed `BUN_INSTALL_CACHE_DIR`, else `$BUN_INSTALL/install/cache` with `BUN_INSTALL` defaulting to `~/.bun`; no tool invocation / delete through the same `os.OpenRoot(parent).RemoveAll(base)` helper as Xcode; installed `node_modules` are APFS clones/copies); `homebrew` (`brew --cache` / `brew cleanup`); `xcode` (fixed `~/Library/Developer/Xcode/DerivedData`, darwin only, Clean = `os.OpenRoot(parent).RemoveAll("DerivedData")`, i.e. delete as the spec says); `cargo-registry` (fixed `~/.cargo/registry`, report only); `rustup` (fixed `~/.rustup/toolchains`, report only; deviation from the spec: per-toolchain usage by repo is deferred, the total is reported).
  - Report-only buckets, same type with `ReportOnly: true` and fixed paths: `huggingface` `~/.cache/huggingface`, `colima` `~/.colima`, `docker` `~/Library/Containers/com.docker.docker/Data`, `codex` `~/.codex`.
  - `func cacheCandidates(ctx context.Context, e Env) []Candidate` — ID `"cache:" + tool.ID`, `Path` from Locate (first non-empty stdout line, must be absolute and exist) or fixed path; `Status` `"ready"` or `"report_only"`; `Commands` evidence line `"clean with: <command>"` or `"report only; <reason>"`. Locate runs through `e.tool(ctx, binary, args...)`, backed by a new injectable field `Env.Tool func(ctx context.Context, binary string, args ...string) ([]byte, error)` (nil = `exec.CommandContext`), like `Env.Git`, with a 5 s timeout; a missing binary or non-zero exit skips the tool silently. Bun applies the environment/default fallback above; other tools with empty output are skipped. A relative resolved location is always skipped.
  - `Apply` for `Cache`: report-only → error `"report only"`; `!e.RunTools` → error `"cache cleanup runs from the driga CLI only"`; else run `Clean` via `e.tool`, set `Commands`, measure `FreedBytes` like Task 4.
  - `size.go` treats `Status == "report_only"` like `History` today.

- [ ] **Step 1: Write failing tests** (inject an `Env.Tool` fake):
  - `TestCacheListsLocatedTool`: fake `go env GOCACHE` → temp dir → candidate `cache:go-build` `ready`.
  - `TestCacheMissingToolSkipped`: runner returns `exec.ErrNotFound` for `uv` and `bun` → neither listed, `List` error nil.
  - `TestCacheReportOnly`: `~/.cache/huggingface` exists → `report_only`; `Apply` → error "report only".
  - `TestCacheServiceRefused`: `RunTools=false` → `Apply` error, runner never called with `clean`.
  - `TestCacheRunsToolCommand`: `RunTools=true` → runner called with `go clean -cache`; `cleanup.log` records it.
- [ ] **Step 2: Run** `go test ./internal/cleanup/ -run TestCache` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/cleanup/` — PASS.

### Task 6: Surfaces, alert and docs

**Files:**
- Modify: `internal/cli/cleanup.go:30,36,136,153`, `internal/cli/top.go:86` (set `RunTools`), `internal/actions/cleanup.go:109,114-120`, `internal/mcp/server.go:172-176,245`, `internal/collector/cleanup.go:51`, `docs/usage.md` (Cleanup section, settings table rows for `cleanup.build_idle` and `cleanup.regenerable`)
- Test: `internal/cli/cleanup_test.go`, `internal/mcp/*_test.go` (existing kind-enum test), `internal/collector/alerts_candidates_test.go`

**Interfaces:**
- Consumes: `cleanup.Build`, `cleanup.Cache`, `Env.RunTools`, `Result.FreedBytes`.
- Kind validation everywhere accepts `worktree, scratch, build, cache, history`; help text lists them in that order. `remove --kind history` error unchanged; `--kind cache --all` skips `report_only` items as `skipped`.
- `openCleanup` sets `env.RunTools = true` (CLI process only). The service never calls `openCleanup`.
- `PrintCleanup` prints kinds in the order above; after removal prints `freed <DecimalBytes(FreedBytes)>` when non-zero. Previews with `--sizes` print `up to <n> bytes`.
- Low-disk alert summary (collector, no new walking; deviation from the spec, which asked to name the largest candidates: the service must not walk directories or run tools, so it points at the commands instead) becomes: `"%s free, below %s. Reclaim build output and tool caches with driga cleanup list --kind build --sizes and driga cleanup list --kind cache --sizes."` plus the existing oldest-worktree sentence.
- Docs: add **Build** and **Cache** bullets matching the spec rules (delete not Trash, excluded names, CLI-only tools, report-only buckets), and the two settings rows.

- [ ] **Step 1: Write failing tests**: CLI `cleanup list --kind build` accepted and `--kind bogus` message lists the five kinds; MCP schema enum equals the five kinds; low-disk summary contains `--kind build`; `remove --kind cache --all --dry-run --yes` over a fake tool prints `skipped` for a report-only bucket.
- [ ] **Step 2: Run** `go test ./internal/cli/ ./internal/mcp/ ./internal/collector/` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./...` and `TZ=UTC go test ./...` — PASS.

### Task 7: End-to-end on the built binary and commit

- [ ] **Step 1:** `go build -o /tmp/driga-e2e ./cmd/driga`. In a scratch tree with `HOME` and the data dir pointed at temp dirs: a git repo with `package.json`, ignored `node_modules/` (10 MiB file), commit dated 30 days ago, a stored worktree via a normal collector pass (`driga collect` or equivalent), and fake `go`/`uv`/`bun` scripts first on `PATH`.
- [ ] **Step 2:** `driga cleanup list --kind build --sizes` shows the candidate with `up to`; `driga cleanup remove <id> --yes` deletes it and prints `freed`; `cleanup.log` has the entry; repo source intact.
- [ ] **Step 3:** `driga cleanup list --kind cache` lists the fake tools; `uv` with empty output is absent; `remove cache:go-build --yes` invokes the fake `go clean -cache` (script writes a marker file).
- [ ] **Step 4:** Prove the tests fail without the fix: revert `build.go`'s `openUnder` check locally and confirm `TestBuildOpenFileBlocks` fails, then restore.
- [ ] **Step 5:** `cd ui && bun run build` is not needed (no UI change); confirm `git status --short` lists only the intended files.
- [ ] **Step 6: Commit once**

```bash
git add internal/platform internal/cleanup internal/config internal/cli/cleanup.go internal/cli/top.go internal/actions/cleanup.go internal/mcp/server.go internal/collector/cleanup.go docs/usage.md docs/specs/2026-11-01-disk-reclaim-design.md docs/superpowers/plans/2026-11-01-disk-reclaim.md
git commit -m "feat(cleanup): reclaim build output and tool caches"
```
