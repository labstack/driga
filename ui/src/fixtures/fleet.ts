// The Fleet spec's fragment over the same fictional fleet as fragments.json:
// what the service answers when the app runs the Fleet spec. Its sessions
// are the sessions source's rows the search fixture already holds; the
// other frames add what the Fleet reads from the machines, flow, orphans,
// projects, activity and memory sources, with their columns as
// internal/panel/sources.go lists them.
import type { Column, Fragment, Frame } from "../panels/types";
import { fragments } from "./fragments";

const col = (name: string, type: Column["type"], unit?: string, hidden?: boolean): Column => ({ name, type, ...(unit && { unit }), ...(hidden && { hidden }) });
const GB = 1024 ** 3;
const MB = 1024 ** 2;

const machines: Frame = {
  columns: [col("machine_id", "string"), col("machine", "string"), col("state", "string"), col("os", "string"), col("agents", "number", "count"), col("inbox", "number", "count"), col("last_seen", "number", "duration")],
  rows: [
    ["studio-id", "studio", "local", "darwin", 7, 4, 0],
    ["build-id", "build-01", "live", "linux", 3, 2, 4000],
    ["laptop-id", "laptop-02", "stale", "darwin", 0, 0, 840000],
  ],
};

// The sessions source's rows, as the search fixture holds them.
const sessions: Frame = fragments.search.results.find(r => r.id === "live")!.frame!;

const flow: Frame = {
  columns: [col("id", "number"), col("parent_id", "number"), col("started_at", "time"), col("ended_at", "time"), col("last_tool", "string"), col("reason", "string"), col("machine_id", "string", undefined, true)],
  rows: [
    [11, null, "2026-03-14T14:18:00Z", null, "Bash(npm run migrate -- --env staging)", "permission", "studio-id"],
    [12, null, "2026-03-14T14:42:00Z", null, "Bash(npm test -- checkout)", "", "studio-id"],
    [13, 12, "2026-03-14T14:50:00Z", null, "Bash(npm run test:payments)", "", "studio-id"],
    [14, null, "2026-03-14T13:00:00Z", null, "", "", "studio-id"],
    [15, null, "2026-03-14T12:00:00Z", null, "Edit(docs/release-notes.md)", "", "studio-id"],
    [16, null, "2026-03-14T14:05:00Z", null, "Edit(billing/events.go)", "question", "build-id"],
    [17, null, "2026-03-14T08:00:00Z", null, "Bash(go test ./index/...)", "", "build-id"],
    [18, null, "2026-03-13T17:00:00Z", "2026-03-13T19:00:00Z", "Edit(site/src/index.md)", "", "studio-id"],
    [19, null, "2026-03-14T11:00:00Z", null, "", "", "studio-id"],
  ],
};

const orphans: Frame = {
  columns: [col("id", "string"), col("signature", "string"), col("root_pid", "number"), col("machine_id", "string"), col("machine", "string"), col("processes", "number", "count"), col("rss_bytes", "number", "bytes"), col("age", "number", "duration"), col("verified", "bool"), col("young", "bool"), col("cwd", "string")],
  rows: [
    ["5150:1", "jest --watch", 5150, "studio-id", "studio", 6, 1288490188, 2700000, false, false, "/home/dev/src/harbor"],
    ["4242:1", "vite dev server", 4242, "studio-id", "studio", 3, 859832320, 5400000, true, false, "/home/dev/src/atlas"],
    ["7001:1", "go build cache daemon", 7001, "build-id", "build-01", 1, 420 * MB, 9000000, true, false, "/home/dev/src/beacon"],
  ],
};

const projects: Frame = {
  columns: [col("id", "number"), col("project", "string"), col("path", "string"), col("branch", "string"), col("machine", "string"), col("live_agents", "number", "count"), col("dirty", "number", "count"), col("unpushed", "number", "count"), col("worktrees", "number", "count"), col("activity", "string"), col("at_risk", "string"), col("machine_id", "string")],
  rows: [
    [1, "harbor", "/home/dev/src/harbor", "fix/checkout-flake", "studio", 2, 3, 1, 1, "active", "", "studio-id"],
    [2, "atlas", "/home/dev/src/atlas", "feat/upload-limits", "studio", 2, 0, 0, 1, "active", "", "studio-id"],
    [3, "beacon", "/home/dev/src/beacon", "chore/deps", "studio", 2, 0, 0, 0, "active", "", "studio-id"],
    [4, "sandbox", "/home/dev/src/sandbox", "", "studio", 1, 0, 0, 0, "quiet", "", "studio-id"],
    [5, "harbor", "/home/dev/src/harbor", "feat/billing-v2", "build-01", 1, 1, 0, 0, "active", "", "build-id"],
    [6, "beacon", "/home/dev/src/beacon", "perf/index", "build-01", 1, 0, 0, 0, "quiet", "", "build-id"],
  ],
};

const activity: Frame = {
  columns: [col("ts", "time"), col("machine", "string"), col("mem_used_bytes", "number", "bytes"), col("mem_total_bytes", "number", "bytes")],
  rows: [
    ["2026-03-14T15:00:00Z", "studio", 25769803776, 34359738368],
    ["2026-03-14T15:00:00Z", "build-01", 14227079168, 68719476736],
    ["2026-03-14T14:59:00Z", "studio", 26843545600, 34359738368],
    ["2026-03-14T14:59:00Z", "build-01", 14100000000, 68719476736],
    ["2026-03-14T14:46:00Z", "laptop-02", 11 * GB, 16 * GB],
  ],
};

const memory: Frame = {
  columns: [col("agent", "string"), col("role", "string"), col("machine", "string"), col("rss_bytes", "number", "bytes")],
  rows: [
    ["claude", "interactive", "studio", 2.6 * GB],
    ["claude", "helper", "studio", 1.1 * GB],
    ["codex", "interactive", "studio", 0.4 * GB],
    ["agy", "headless", "studio", 130 * MB],
    ["claude", "interactive", "build-01", 2.2 * GB],
    ["codex", "helper", "build-01", 420 * MB],
  ],
};

const observed_at = "2026-03-14T15:00:00Z";

const result = (id: string, frame: Frame) => ({ id, status: "ok" as const, frame });

/** fleet is the busy fleet: three machines, one unreachable, every session
 * state, leftovers on two machines. */
export const fleet: Fragment = {
  dashboard: { name: "Fleet", panels: [] },
  results: [result("machines", machines), result("sessions", sessions), result("flow", flow), result("orphans", orphans), result("projects", projects), result("activity", activity), result("memory", memory)],
  observed_at,
  local_machine: "studio-id",
};

/** fleetQuiet is the fleet with nothing needing the person: the sessions
 * that work or rest, every machine reachable, nothing left over. */
export const fleetQuiet: Fragment = {
  ...fleet,
  results: [
    result("machines", { ...machines, rows: machines.rows.map(r => r[2] === "stale" ? [...r.slice(0, 2), "live", ...r.slice(3, 6), 2000] : r) }),
    result("sessions", { ...sessions, rows: sessions.rows.filter(r => ["working", "idle", "finished"].includes(String(r[11]))) }),
    result("flow", flow), result("orphans", { ...orphans, rows: [] }), result("projects", projects), result("activity", activity), result("memory", memory),
  ],
};

/** fleetEmpty is a service that knows no machines yet. */
export const fleetEmpty: Fragment = { ...fleet, results: fleet.results.map(r => ({ ...r, frame: { ...r.frame!, rows: [] } })) };
