// A fictional fleet for the Fleet view: three machines, five projects, one
// of them on two machines, fourteen sessions in every state, two spawn
// pairs, one file overlap across machines, and leftovers. Times are
// relative to the stories' clock, which the preview pins Date.now to.
import { storyNow } from "../fixtures/fragments";
import type { FleetData, FleetMachine, FleetProject, FleetSession, FleetTurn } from "./FleetTypes";

const ago = (minutes: number) => new Date(storyNow - minutes * 60_000).toISOString();
const MB = 1024 * 1024;
const GB = 1024 * MB;

/** turns repeats a pattern to a length, so each session has its own history. */
function turns(pattern: string, n: number): FleetTurn[] {
  const glyph: Record<string, FleetTurn> = { ".": "ok", "w": "waited", "x": "failed" };
  return Array.from({ length: n }, (_, i) => glyph[pattern[i % pattern.length]] ?? "ok");
}

export const machines: FleetMachine[] = [
  { id: "studio", name: "studio", kind: "desktop", local: true, reachable: true, lastSeen: ago(0), memoryUsed: 25 * GB, memoryTotal: 36 * GB, leftover: { processes: 3, memory: 1.8 * GB }, memoryByRole: [{ label: "sessions", value: 10.4 * GB }, { label: "MCP servers", value: 3.1 * GB }, { label: "leftovers", value: 1.8 * GB }] },
  { id: "build-01", name: "build-01", kind: "server", reachable: true, lastSeen: ago(0), memoryUsed: 41 * GB, memoryTotal: 64 * GB, leftover: { processes: 1, memory: 420 * MB } },
  { id: "laptop-02", name: "laptop-02", kind: "laptop", reachable: false, lastSeen: ago(14), memoryUsed: 11 * GB, memoryTotal: 16 * GB },
];

export const projects: FleetProject[] = [
  { id: "atlas", name: "atlas", repo: "example.com/atlas" },
  { id: "harbor", name: "harbor", repo: "example.com/harbor" },
  { id: "beacon", name: "beacon", repo: "example.com/beacon" },
  { id: "sandbox", name: "sandbox", repo: "example.com/sandbox" },
  { id: "ledger", name: "ledger", repo: "example.com/ledger" },
];

export const sessions: FleetSession[] = [
  // studio · atlas
  { id: "112", title: "Fix the flaky lifeline test", state: "working", agent: "codex", project: "atlas", machine: "studio", branch: "fix/lifeline", memory: 412 * MB, files: ["internal/lifeline/lifeline_test.go", "internal/lifeline/clock.go"], lastTool: "Edit(internal/lifeline/lifeline_test.go)", lastActivity: ago(1), started: ago(27), turns: turns("......w.........", 30) },
  { id: "113", title: "Write the release notes", state: "waiting", agent: "claude", project: "atlas", machine: "studio", branch: "main", memory: 388 * MB, files: ["CHANGELOG.md"], lastTool: "Bash(git push origin main)", lastActivity: ago(6), started: ago(52), turns: turns("........w....", 22), waiting: "Permission: Bash(git push origin main)" },
  { id: "118", title: "Review the changelog wording", state: "working", agent: "claude", project: "atlas", machine: "studio", branch: "main", memory: 221 * MB, parent: "113", files: [], lastTool: "Read(CHANGELOG.md)", lastActivity: ago(2), started: ago(9), turns: turns(".....", 7) },
  // studio · harbor
  { id: "104", title: "Add retry to the uploader", state: "stuck", inferred: true, evidence: "busy, no output for 23m, CPU idle", agent: "claude", project: "harbor", machine: "studio", branch: "feat/retry", memory: 1.2 * GB, files: ["pkg/upload/retry.go", "pkg/upload/client.go"], lastTool: "Bash(go test ./pkg/upload/...)", lastActivity: ago(23), started: ago(140), turns: turns("..........x..x", 30) },
  { id: "107", title: "Harbor docs for the CLI", state: "idle", agent: "gemini", project: "harbor", machine: "studio", branch: "docs/cli", memory: 150 * MB, files: ["docs/cli.md"], lastTool: "Edit(docs/cli.md)", lastActivity: ago(48), started: ago(190), turns: turns("....", 12) },
  // studio · sandbox
  { id: "121", title: "Try the new tokenizer", state: "finished", agent: "agy", project: "sandbox", machine: "studio", branch: "main", memory: 96 * MB, files: ["scratch/tokens.py"], lastTool: "Bash(python scratch/tokens.py)", lastActivity: ago(31), started: ago(60), turns: turns("...", 9) },
  // build-01 · atlas (the same repo as on studio)
  { id: "090", title: "Migrate the panel engine", state: "working", agent: "codex", project: "atlas", machine: "build-01", branch: "feat/engine", memory: 890 * MB, files: ["internal/panel/engine.go", "internal/lifeline/clock.go"], lastTool: "Edit(internal/panel/engine.go)", lastActivity: ago(0), started: ago(75), turns: turns("............", 30) },
  { id: "091", title: "Benchmark the sankey build", state: "idle", agent: "copilot", project: "atlas", machine: "build-01", branch: "perf/sankey", memory: 300 * MB, files: ["ui/src/panels/charts.ts"], lastTool: "Bash(bun run bench)", lastActivity: ago(40), started: ago(120), turns: turns(".....x", 18) },
  // build-01 · beacon
  { id: "095", title: "Beacon alert routing", state: "working", agent: "claude", project: "beacon", machine: "build-01", branch: "feat/routing", memory: 640 * MB, files: ["route/alerts.go", "route/alerts_test.go"], lastTool: "Edit(route/alerts_test.go)", lastActivity: ago(1), started: ago(44), turns: turns("........w.", 30) },
  { id: "099", title: "Check the pager API", state: "waiting", agent: "claude", project: "beacon", machine: "build-01", branch: "feat/routing", memory: 210 * MB, parent: "095", files: [], lastTool: "WebFetch(pager.example.com/docs)", lastActivity: ago(4), started: ago(11), turns: turns("...w", 5), waiting: "Question: which pager account should alerts use?" },
  { id: "096", title: "Beacon load test", state: "stuck", inferred: true, evidence: "busy, no output for 35m, CPU idle", agent: "codex", project: "beacon", machine: "build-01", branch: "test/load", memory: 1.5 * GB, files: ["load/run.go"], lastTool: "Bash(go run ./load)", lastActivity: ago(35), started: ago(95), turns: turns("..............x", 30) },
  // laptop-02 (unreachable: last known)
  { id: "080", title: "Harbor TLS rotation", state: "working", inferred: true, agent: "claude", project: "harbor", machine: "laptop-02", branch: "ops/tls", memory: 500 * MB, files: ["pkg/tls/rotate.go"], lastTool: "Edit(pkg/tls/rotate.go)", lastActivity: ago(14), started: ago(80), turns: turns("........", 30) },
  { id: "081", title: "Harbor smoke tests", state: "finished", inferred: true, agent: "codex", project: "harbor", machine: "laptop-02", branch: "ops/tls", memory: 120 * MB, files: ["pkg/tls/rotate_test.go"], lastTool: "Bash(go test ./pkg/tls/...)", lastActivity: ago(20), started: ago(50), turns: turns("......", 14) },
  { id: "082", title: "Ledger import fixtures", state: "waiting", inferred: true, agent: "gemini", project: "ledger", machine: "laptop-02", branch: "main", memory: 240 * MB, files: ["fixtures/import.json"], lastTool: "Write(fixtures/import.json)", lastActivity: ago(16), started: ago(33), turns: turns(".....w", 12), waiting: "Permission: Write(fixtures/import.json)" },
];

/** busy is the whole fictional fleet. */
export const busy: FleetData = { machines, projects, sessions };

/** oneMachine is the local machine alone, as a new install looks. */
export const oneMachine: FleetData = { machines: machines.filter(m => m.id === "studio"), projects, sessions: sessions.filter(s => s.machine === "studio") };

/** unreachable is a fleet whose one machine Driga cannot reach: every
 * session shows as last known. */
export const unreachable: FleetData = { machines: machines.filter(m => m.id === "laptop-02"), projects, sessions: sessions.filter(s => s.machine === "laptop-02") };

/** bare is the fleet as the service sends it today: no turns, no files, no
 * started time, no waiting reason beyond what the inbox says. */
export const bare: FleetData = { machines: machines.map(m => ({ ...m, memoryByRole: undefined })), projects, sessions: sessions.map(({ turns: _turns, files: _files, ...s }) => s) };

export const empty: FleetData = { machines: [], projects: [], sessions: [] };

/** A session that waits for the person, for the inspector stories. */
export const waitingSession = sessions.find(s => s.id === "113")!;
