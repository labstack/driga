# Agents-first machine view Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The Machine view, its JSON and its alerts lead with what agents cost (proven versus likely, per session, without a session), and fold every other app into one "Other apps" bucket.

**Architecture:** One new pure classifier in `internal/derive` puts every sampled process into exactly one bucket, using the attribution proofs that `view.machineApps` uses today (moved, not rewritten), agent signatures, MCP hosts, and an extension-host rule. `view` builds the new `MachineView` fields from it; CLI, TUI, web, MCP and the collector's pressure alert read those fields.

**Tech Stack:** Go 1.27 (cgo-free), Bubble Tea v2 / Lip Gloss v2, html/template + htmx, modernc sqlite.

**Spec:** `docs/superpowers/specs/2026-10-20-driga-agents-first-machine-design.md` (revises `docs/superpowers/specs/2026-10-20-driga-machine-health-design.md`).

## Global Constraints

- Not curve-fitted (design §2a): no product names, absolute GB or process-count thresholds tuned to the maintainer's machine; rules from mechanisms (signatures data, parent chains, proof rules). Fixtures are synthetic or held out, never the maintainer's process list.
- Every sampled, non-zombie process is in exactly one bucket, checked in this order: attributed to a session; proven agent process without a session; likely agent work; other app. Totals equal the sum of buckets.
- Role-only signature matches that name no agent (`desktop-app`, `generic-mcp-launcher`, `driga-internal`) are not agent evidence on their own.
- JSON is additive only: schema stays 2; `top_memory`, `top_cpu` and trend state value `"leaking"` stay unchanged. Only displayed wording changes.
- Displayed "leak"/"leaking" becomes "observed growth".
- No new process scans or per-process reads; the existing heap-budget and cost tests must pass.
- Tests never touch real `~/.claude`, `~/.codex`, `~/.copilot`, `~/.gemini` or data dirs; use `DRIGA_DATA_DIR`, `DRIGA_SPOOL_DIR`, `DRIGA_CONFIG`, temp HOME. TUI goldens pin `time.Local` before any fixture goroutine starts.
- Before the PR: `gofmt -l`, `go vet ./...`, `go test ./...`, `TZ=UTC go test ./...`, `go test -race ./internal/derive ./internal/view ./internal/collector ./internal/web ./internal/tui`.

## Scope rulings (author's calls, against the spec)

- "Started from" is shown in the Machine view and its JSON only; agent lists, the Flow map and alert text follow in a later change.
- Agent evidence from network connections to AI model services, and a user "treat as agent" config mark, are deferred: driga stores no outbound connection data today, and the spec forbids new per-process collection.

## Review Focus

1. A non-agent app (e.g. an editor) spawning a plain `node` worker that is not an MCP server: must stay in Other apps, not likely. (Task 1 test `TestBucketsEditorWorkerIsOther`.)
2. PID reuse / parent started after child: the parent-chain walk must stop, so a stale PID never makes a process "agent". (Task 1 test `TestBucketsRejectsImpossibleParent`.)
3. Two sessions in the same project and a process matched only by cwd: no session attribution; the process falls to "agent without session" only if it is itself an agent process, else Other. (Task 1 test `TestBucketsAmbiguousCwdNotAttributed`.)
4. Machine with no agents at all: summary line and alert say agents hold 0 and nothing is implied. (Task 2 test `TestAgentShareNoAgents`, Task 4 test `TestPressureSummaryAgentsSmallShare`.)
5. Remote (fleet) machines whose peer runs an older driga without the new fields: views render without panics and show the old sections' data. (Task 3 test `TestMachineViewOlderPeerFields`.)

---

### Task 1: Classify every process into one bucket

**Files:**
- Create: `internal/derive/buckets.go`
- Create: `internal/derive/buckets_test.go`
- Modify: `internal/view/machine_health.go` (move the parent-chain and cwd proof walk out of `machineApps` into derive; `machineApps` calls it)

**Interfaces:**
- Produces:
  ```go
  type Bucket string
  const (
      BucketSession     Bucket = "session"      // attributed to a live session
      BucketAgent       Bucket = "agent"        // proven agent process, no session
      BucketLikely      Bucket = "likely"       // likely agent work under a non-agent host
      BucketOther       Bucket = "other"
  )
  type ProcAttribution struct {
      SessionID int64
      Project   string
      Proof     string // "parent_chain" | "cwd_started_after"
  }
  type ProcPlacement struct {
      PID         int32
      Bucket      Bucket
      Attribution *ProcAttribution
      Bundle      string // nearest .app bundle on the parent chain, "" if none
      SharedHost  int32  // for BucketLikely: PID of the non-agent host (e.g. extension host), else 0
      Memory      uint64
      Measure     string
  }
  // PlaceProcesses places every non-zombie process exactly once.
  func PlaceProcesses(procs []model.Proc, sessions []model.Session, sigs *signatures.Set) map[int32]ProcPlacement
  ```
- Rules (exact):
  - Session: same proofs as today's `machineApps` (parent chain reaches a live session's PID with matching start; else unique most-specific cwd match for a process started after the session). Keep the PID-reuse guard (`parent.StartTime.After(cur.StartTime)` stops the walk).
  - Agent: not session, and the process or any ancestor is a proven agent process: signature match with non-empty `Agent`, or stored `p.Agent != ""`, with role in interactive, headless, host, service, helper.
  - Likely: not session or agent, and the process or an ancestor (below the nearest non-agent app process) is an MCP server: `describeMCP` gives a package/command whose name contains the token `mcp` (case-insensitive, word or `-`/`_` boundary), or the `generic-mcp-launcher` signature matches. `SharedHost` = that MCP server's parent PID. The host process itself stays Other.
  - Other: everything else.

- [ ] **Step 1: Write the failing tests** in `internal/derive/buckets_test.go`, each building a synthetic `[]model.Proc` and `[]model.Session` and asserting `PlaceProcesses` buckets:
  - `TestBucketsParentChainSession`: `claude` (session PID) → `node mcp-server-x` child → both `BucketSession`, child proof `"parent_chain"`.
  - `TestBucketsCwdStartedAfter`: dev server in the session's cwd, started after the session, no parent link → `BucketSession`, proof `"cwd_started_after"`.
  - `TestBucketsAmbiguousCwdNotAttributed`: two live sessions with the same cwd → not session; a plain `node server.js` → `BucketOther`.
  - `TestBucketsAgentWithoutSession`: `codex app-server` with no session, plus its child → both `BucketAgent`.
  - `TestBucketsEditorExtensionHostMCP`: `/Applications/Editor.app/.../Editor Helper (Plugin)` → child `node /x/node_modules/@acme/weather-mcp/dist/index.js` → host `BucketOther`, child `BucketLikely` with `SharedHost` = host PID, `Bundle` = `/Applications/Editor.app`.
  - `TestBucketsEditorWorkerIsOther`: same host → child `node /x/tsserver.js` → `BucketOther`.
  - `TestBucketsEditorLaunchedAgentIsSession`: `Editor.app` → extension host → `claude` with a live session → `BucketSession`, `Bundle` = `/Applications/Editor.app`.
  - `TestBucketsRoleOnlySignatureNotAgent`: a `.app` matching only `desktop-app` → `BucketOther`.
  - `TestBucketsRejectsImpossibleParent`: child whose PPID is a session PID but the parent started after the child → not session.
  - `TestBucketsEveryProcessOnce`: random mix of the above; `len(result) == non-zombie count`.
- [ ] **Step 2: Run** `go test ./internal/derive -run TestBuckets` — Expected: FAIL, `undefined: PlaceProcesses`.
- [ ] **Step 3: Implement** `PlaceProcesses` in `internal/derive/buckets.go`; move the proof walk from `view.machineApps` into it and make `machineApps` use `ProcPlacement.Attribution` and `.Bundle` (its outputs unchanged).
- [ ] **Step 4: Run** `go test ./internal/derive ./internal/view` — Expected: PASS, existing machine tests unchanged.
- [ ] **Step 5: Commit** `feat(derive): place every process in one agent bucket`.

### Task 2: Agents' share, session costs and other apps in MachineView

**Files:**
- Create: `internal/view/agent_share.go`, `internal/view/agent_share_test.go`
- Modify: `internal/view/view.go` (`MachineView` new fields), `internal/view/builders.go:492` (fill them), `internal/view/fleet.go:288` (copy them for remote machines)

**Interfaces:**
- Consumes: `derive.PlaceProcesses`, `derive.ProcPlacement`.
- Produces (new `MachineView` fields, JSON names exact):
  ```go
  type AgentShare struct {
      Memory        uint64  `json:"memory_bytes"`        // session + agent buckets
      LikelyMemory  uint64  `json:"likely_memory_bytes"`
      MachineMemory uint64  `json:"machine_memory_bytes"` // Sample.MemUsed
      MemoryMeasure string  `json:"memory_measure"`
      CPUPercent    float64 `json:"cpu_percent"`
      Processes     int     `json:"processes"`
      MCPServers    int     `json:"mcp_servers"`          // sum of MCPServers.Commands counts
  }
  type SessionCost struct {
      SessionID   int64            `json:"session_id"`
      Adapter     string           `json:"adapter"`
      Project     string           `json:"project"`
      StartedFrom string           `json:"started_from,omitempty"` // bundle name without .app, e.g. "Visual Studio Code"
      Memory      uint64           `json:"memory_bytes"`
      CPUPercent  float64          `json:"cpu_percent"`
      Processes   []MachineProcess `json:"processes"`
  }
  // in MachineView:
  AgentShare   AgentShare    `json:"agent_share"`
  SessionCosts []SessionCost `json:"session_costs"`
  AgentNoSession []MachineApp `json:"agent_processes_without_session"` // grouped by ProcessName
  LikelyAgent  []MachineApp  `json:"likely_agent_work"`  // grouped by shared host app; Name "<app> (shared with editor)"
  OtherApps    MachineApp    `json:"other_apps"`         // ID "other", Name "Other apps"; Processes empty; totals only
  ```
  `TopMemory`/`TopCPU` stay, computed as today.
- [ ] **Step 1: Write the failing tests** in `agent_share_test.go` using `BuildMachine`-level fixtures (reuse the existing machine fixture helpers in `internal/view`):
  - `TestAgentShareTotalsEqualBuckets`: `AgentShare.Memory + LikelyMemory + OtherApps.Memory == sum of all process memory`.
  - `TestSessionCostsIncludeHelpers`: session with two attributed children → one `SessionCost` with 3 processes, sorted by memory desc.
  - `TestSessionCostStartedFrom`: editor-launched session → `StartedFrom == "Editor"`; terminal-launched with no bundle → `""`.
  - `TestAgentShareNoAgents`: no agent processes → `AgentShare.Memory == 0`, `SessionCosts` empty (not nil in JSON: `[]`).
  - `TestMachineJSONAdditive`: marshal `MachineView`; keys `top_memory`, `top_cpu`, `agent_share`, `session_costs`, `agent_processes_without_session`, `likely_agent_work`, `other_apps` all present; schema version unchanged.
- [ ] **Step 2: Run** `go test ./internal/view -run 'TestAgentShare|TestSessionCost|TestMachineJSONAdditive'` — Expected: FAIL, undefined fields.
- [ ] **Step 3: Implement** in `agent_share.go`; call from `builders.go` next to `machineApps`; copy in `fleet.go`.
- [ ] **Step 4: Run** `go test ./internal/view ./internal/mcp` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(view): agents' share, session costs and other apps`.

### Task 3: Surfaces lead with agents

**Files:**
- Modify: `internal/tui/machine.go`, `internal/view/render.go` (CLI text around line 216), `internal/web/templates/dashboard.html` (machines page), `internal/web/pages.go` if a FuncMap helper is needed, `internal/mcp/server.go:146` (tool description), `internal/view/machine_health.go` (`MCPTrendMarker` wording)
- Test: TUI goldens `internal/tui/testdata/machine-*.golden` (regenerate), `internal/web/machine_health_test.go`, `internal/view` render tests

**Interfaces:**
- Consumes: Task 2 fields.
- Order on every surface (exact): summary line; "Sessions by cost" (top 10, each expandable to processes with proof text `started by` for `parent_chain` and `in project, started later` for `cwd_started_after`); "Agent processes without a session" (with the existing MCP roll-up, pile-up and growth rows); "Likely agent work (shared with editor)" when non-empty; "Other apps" (collapsed; expands to the existing top-10 memory and CPU lists); vitals detail; "Agent memory by role".
- Summary line copy (exact format): `agents {Memory} of {MachineMemory} ({pct}%) · likely +{LikelyMemory} · {CPUPercent}% CPU · {Processes} processes · {MCPServers} MCP servers · pressure {level} · swap {used}/{total} · disk {free} free`; omit `likely +…` when 0. Keep the footprint note.
- Helper label: `MCPCommandSummary` says `{n} servers` only when the command passes Task 1's MCP token rule, else `{n} helper processes`. Test `TestMCPCommandSummaryHelperLabel`.
- Wording: `MCPTrendMarker` shows `observed growth ↑ {rate}/h since {HH:MM}` instead of `leaking ↑ …`; JSON state stays `"leaking"`.
- [ ] **Step 1: Write the failing tests:**
  - `TestRenderMachineAgentsFirst` (view): CLI text's first non-header line starts with `agents `; "Other apps" appears after "Sessions by cost".
  - `TestMCPTrendMarkerObservedGrowth` (view): marker for state `"leaking"` contains `observed growth` and not `leak`.
  - `TestMachinePageAgentsFirst` (web): rendered machines page contains the summary line before `Other apps`, and `Other apps` is inside a collapsed `<details>`.
  - `TestMachineViewOlderPeerFields` (web and TUI): a remote `MachineView` with zero-value new fields renders without panic and shows `Other apps` totals from `TopMemory`.
  - TUI goldens: regenerate with `-update` after implementation; review the diff by eye for the order above at 80 and 140 columns.
- [ ] **Step 2: Run** `go test ./internal/view ./internal/web ./internal/tui` — Expected: FAIL on the new tests.
- [ ] **Step 3: Implement** the three surfaces and the MCP tool description ("Read agents' share of machine memory and CPU, cost by session, agent processes without a session, likely agent work, other apps, MCP server roll-ups and pile-ups.").
- [ ] **Step 4: Run** `go test ./internal/tui -update` then `go test ./internal/view ./internal/web ./internal/tui ./internal/mcp` — Expected: PASS; golden diff shows the new order.
- [ ] **Step 5: Commit** `feat(machine): lead the machine view with agents`.

### Task 4: Pressure alert leads with the agents' part

**Files:**
- Modify: `internal/collector/machine_health.go` (`memoryPressureSummary`), `internal/collector/pass.go:200` (pass sessions so the summary can compute the share)
- Test: `internal/collector/machine_health_test.go`

**Interfaces:**
- Consumes: `derive.PlaceProcesses`.
- Signature change: `memoryPressureSummary(samples [][]model.Proc, sessions []model.Session, now time.Time, trends []derive.MCPTrend) string`; `memoryPressureInbox` gains a `sessions []model.Session` parameter supplied by `pass.go` from the transaction's live sessions.
- Copy (exact openings):
  - agents ≥ 1 byte: `Critical memory pressure for two samples. Agents hold {X} of {Y} in use ({pct}%), {n} MCP servers.` then the existing pile-up sentence, then `Largest observed growth: …` (renamed from `Largest MCP leak:`).
  - no agent memory: `Critical memory pressure for two samples. Agents hold none of the {Y} in use; other apps are the cause.`
- [ ] **Step 1: Write the failing tests:** `TestPressureSummaryLeadsWithAgents` (agents 4 GiB of 16 GiB → opening contains `Agents hold 4.0 GiB of 16.0 GiB in use (25%)`), `TestPressureSummaryAgentsSmallShare` (no agent processes → `Agents hold none`), `TestPressureSummaryObservedGrowthWording` (no `leak` substring).
- [ ] **Step 2: Run** `go test ./internal/collector -run TestPressureSummary` — Expected: FAIL.
- [ ] **Step 3: Implement.** `low_disk` text is unchanged.
- [ ] **Step 4: Run** `go test ./internal/collector` and `go test -race ./internal/collector` — Expected: PASS, including the heap-budget and cost tests.
- [ ] **Step 5: Commit** `feat(alerts): memory pressure alert names the agents' share`.

### Task 5: Docs

**Files:**
- Modify: `docs/machine-health.md` (new buckets, rules, JSON fields, wording), `README.md` (machine section, one paragraph)

- [ ] **Step 1:** Document the four buckets, the order rule, the MCP token rule, "shared with editor", and the deferred items from the spec.
- [ ] **Step 2: Run** the full pre-PR checks from Global Constraints — Expected: all PASS.
- [ ] **Step 3: Commit** `docs(machine): agents-first buckets`.
