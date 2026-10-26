// The Fleet spec and the mapping from service frames to the shipping view.
import { rows, type Dashboard, type Fragment, type Row } from "../panels/types";
import { stateOf } from "../widgets/StatusBadge";
import type { State } from "../widgets/types";
import type { FleetData, FleetMachine, FleetProject, FleetSession } from "./FleetTypes";

/** fleetSpec reads the fleet: machines, every live session with the flow's
 * extras (parent, start, last tool), leftovers, projects, each machine's
 * CPU, memory and disk pressure, agent memory by role and project tool activity. */
export const fleetSpec: Dashboard = {
  name: "Fleet",
  panels: [
    { id: "machines", title: "Machines", viz: "table", source: "machines", limit: 200 },
    { id: "sessions", title: "Sessions", viz: "table", source: "sessions", limit: 200 },
    { id: "flow", title: "Flow", viz: "table", source: "flow", columns: ["id", "parent_id", "started_at", "ended_at", "last_tool", "reason", "machine_id"], limit: 200 },
    { id: "orphans", title: "Leftover", viz: "table", source: "orphans", limit: 200 },
    { id: "projects", title: "Projects", viz: "table", source: "projects", limit: 200 },
    { id: "memory", title: "Memory by role", viz: "table", source: "memory", limit: 200 },
    { id: "heatmap", title: "Tool calls by project", viz: "timeseries", source: "activity_heatmap", by: "project", measure: "sum:tool_calls" },
  ],
};

const text = (v: unknown) => (v === null || v === undefined ? "" : String(v));
const num = (v: unknown) => (typeof v === "number" ? v : 0);
const knownNum = (v: unknown) => (typeof v === "number" ? v : undefined);
const sessionKey = (machine: unknown, id: unknown) => JSON.stringify([text(machine), text(id)]);

function editedFiles(value: unknown): string[] {
  try {
    const files: unknown = JSON.parse(text(value));
    return Array.isArray(files) ? files.filter((file): file is string => typeof file === "string") : [];
  } catch {
    return [];
  }
}

/** stateFor reads a session's label as the Fleet draws it: the service's
 * states as they are, dead as stuck (its turn is open with no process), and
 * what it cannot tell as idle, inferred. */
export function stateFor(label: string): { state: State; label?: string; inferred: boolean } {
  const read = stateOf(label);
  if (read.state) return { state: read.state, inferred: read.inferred };
  if (read.label === "dead") return { state: "stuck", label: "dead", inferred: true };
  return { state: "idle", label: read.label || undefined, inferred: true };
}

/** fleetOf maps the Fleet spec's frames, and the inbox's waits when the
 * app has them, onto the view's data. */
export function fleetOf(fragment: Fragment, inbox?: Fragment | null): FleetData {
  const frame = (id: string): Row[] => rows(fragment.results.find(r => r.id === id)?.frame);
  const observed = Date.parse(fragment.observed_at) || Date.now();
  const leftovers = new Map<string, { trees: number; processes: number; memory: number }>();
  for (const r of frame("orphans")) {
    const l = leftovers.get(text(r.machine_id)) ?? { trees: 0, processes: 0, memory: 0 };
    leftovers.set(text(r.machine_id), { trees: l.trees + 1, processes: l.processes + num(r.processes), memory: l.memory + num(r.rss_bytes) });
  }
  const byRole = new Map<string, { label: string; value: number }[]>();
  for (const r of frame("memory")) {
    const label = [text(r.agent), text(r.role)].filter(Boolean).join(" · ");
    byRole.set(text(r.machine), [...(byRole.get(text(r.machine)) ?? []), { label, value: num(r.rss_bytes) }]);
  }
  const machines: FleetMachine[] = frame("machines").map(r => {
    const id = text(r.machine_id), name = text(r.machine), state = text(r.state);
    return {
      id, name,
      kind: text(r.os) === "linux" ? "server" : "desktop",
      local: state === "local",
      reachable: state === "local" || state === "live",
      lastSeen: new Date(observed - num(r.last_seen)).toISOString(),
      memoryUsed: num(r.mem_used_bytes), memoryTotal: num(r.mem_total_bytes),
      cpuPercent: typeof r.cpu_percent === "number" ? r.cpu_percent : undefined,
      diskFree: knownNum(r.disk_free_bytes), diskTotal: knownNum(r.disk_total_bytes),
      leftover: leftovers.get(id),
      memoryByRole: byRole.get(name),
    };
  });
  const projects: FleetProject[] = [];
  for (const r of frame("projects")) {
    const name = text(r.project);
    if (name && !projects.some(p => p.id === name)) projects.push({ id: name, name, repo: text(r.path) });
  }
  const flow = new Map(frame("flow").map(r => [sessionKey(r.machine_id, r.id), r]));
  const waits = new Map<string, string>();
  for (const r of rows(inbox?.results[0]?.frame)) {
    if (r.kind === "waiting" && text(r.session_id) && text(r.session_id) !== "0") waits.set(sessionKey(r.machine_id, r.session_id), [r.reason || "waiting", r.tool, r.detail].filter(Boolean).join(" · "));
  }
  const sessions: FleetSession[] = frame("sessions").map(r => {
    const id = text(r.id);
    const key = sessionKey(r.machine_id, id);
    const f = flow.get(key);
    const read = stateFor(text(r.label));
    return {
      id, title: text(r.title) || `Session ${id}`,
      state: read.state, label: read.label, inferred: read.inferred || r.inferred === true, evidence: text(r.evidence) || undefined,
      agent: text(r.agent), project: text(r.project), machine: text(r.machine_id), branch: text(r.branch) || undefined,
      memory: num(r.mem_bytes),
      turns: text(r.turns).split(",").filter((t): t is "ok" | "waited" | "failed" => ["ok", "waited", "failed"].includes(t)),
      files: editedFiles(r.edited),
      parent: f && text(f.parent_id) && text(f.parent_id) !== "0" ? text(f.parent_id) : undefined,
      lastTool: text(f?.last_tool) || undefined,
      lastActivity: text(r.last_activity_at) || fragment.observed_at,
      started: text(f?.started_at) || undefined,
      waiting: read.state === "waiting" ? waits.get(key) ?? (text(f?.reason) || undefined) : undefined,
    };
  });
  // A project a session names that the projects source does not list.
  for (const s of sessions) if (s.project && !projects.some(p => p.id === s.project)) projects.push({ id: s.project, name: s.project, repo: "" });
  return { machines, projects, sessions };
}
