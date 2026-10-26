// Pure derivations the Fleet view shares: machines in a stable order, their
// projects and sessions grouped, subagents under their parent, file overlaps
// between sessions, counts by state, and the shapes the widgets take.
// Nothing here renders.
import { bytes, relative } from "../panels/format";
import type { Machine } from "../widgets/MachineCard";
import type { TrackerCell } from "../widgets/TrackerBar";
import { agentLabel, type State } from "../widgets/types";
import { states, type FleetData, type FleetMachine, type FleetProject, type FleetSession, type FleetState } from "./FleetTypes";

export { plural } from "../panels/format";

export type FleetProjectGroup = { project: FleetProject; sessions: FleetSession[]; memory: number; waiting: boolean };

export type FleetMachineGroup = { machine: FleetMachine; projects: FleetProjectGroup[]; memory: number; counts: Record<FleetState, number> };

/** sortedMachines puts the local machine first, then the rest by name, so
 * nothing reshuffles between refreshes. */
export function sortedMachines(machines: FleetMachine[]): FleetMachine[] {
  return [...machines].sort((a, b) => Number(!!b.local) - Number(!!a.local) || a.name.localeCompare(b.name));
}

/** byState orders sessions by what needs the person first, then by title. */
export function byState(a: FleetSession, b: FleetSession): number {
  return states[a.state].rank - states[b.state].rank || a.title.localeCompare(b.title);
}

export function emptyCounts(): Record<FleetState, number> {
  return { waiting: 0, stuck: 0, working: 0, idle: 0, finished: 0 };
}

export function counts(sessions: FleetSession[]): Record<FleetState, number> {
  const c = emptyCounts();
  for (const s of sessions) c[s.state]++;
  return c;
}

/** needsYou counts the sessions a person must act on. */
export function needsYou(sessions: FleetSession[]): number {
  return sessions.filter(s => s.state === "waiting" || s.state === "stuck").length;
}

/** groupFleet nests sessions under their project under their machine, in
 * stable order: machines local first then by name, projects by name,
 * sessions by state rank then title. A machine with no sessions still
 * appears, with no projects. */
function groupFleet({ machines, projects, sessions }: FleetData): FleetMachineGroup[] {
  const byId = new Map(projects.map(p => [p.id, p]));
  return sortedMachines(machines).map(machine => {
    const own = sessions.filter(s => s.machine === machine.id);
    const groups = new Map<string, FleetSession[]>();
    for (const s of own) groups.set(s.project, [...(groups.get(s.project) ?? []), s]);
    const grouped: FleetProjectGroup[] = [...groups.entries()]
      .map(([id, list]) => ({ project: byId.get(id) ?? { id, name: id, repo: "" }, sessions: list.sort(byState), memory: list.reduce((n, s) => n + s.memory, 0), waiting: list.some(s => s.state === "waiting") }))
      .sort((a, b) => a.project.name.localeCompare(b.project.name));
    return { machine, projects: grouped, memory: own.reduce((n, s) => n + s.memory, 0), counts: counts(own) };
  });
}

/** overlaps finds pairs of sessions of one project editing the same file,
 * each pair once, with the files they share. */
function overlapIndex(sessions: FleetSession[]): Map<FleetSession, { with: FleetSession; files: string[] }[]> {
  const pairs = new Map<FleetSession, Map<FleetSession,string[]>>(sessions.map(s => [s, new Map()]));
  const owners = new Map<string, FleetSession[]>();
  for (const s of sessions) for (const file of new Set(s.files ?? [])) {
    const key = JSON.stringify([s.machine,s.project,file]);
    const previous = owners.get(key) ?? [];
    for (const other of previous) {
      for (const [a,b] of [[s,other],[other,s]]) {
        const links = pairs.get(a)!;
        const files = links.get(b) ?? []; files.push(file); links.set(b,files);
      }
    }
    previous.push(s); owners.set(key,previous);
  }
  return new Map([...pairs].map(([s,others]) => [s,[...others].map(([withSession,files]) => ({with:withSession,files}))]));
}

/** A session's identity always includes its machine. */
export function sessionKey(s: Pick<FleetSession, "machine" | "id">): string {
  return JSON.stringify([s.machine, s.id]);
}

export function sessionSelection(key: string): { kind: "session"; machine: string; id: string } | null {
  try {
    const value: unknown = JSON.parse(key);
    if (Array.isArray(value) && value.length === 2 && value.every(x => typeof x === "string")) return { kind: "session", machine: value[0], id: value[1] };
  } catch { /* Invalid identities select nothing. */ }
  return null;
}

/** Index the complete fleet once. Relationship queries never require callers
 * to prepare a machine or project scope, including cross-project ancestry. */
export class FleetRelationships {
  readonly groups: FleetMachineGroup[];
  readonly machines: FleetMachine[];
  readonly projects: FleetProject[];
  private readonly sessions = new Map<string, FleetSession>();
  private readonly childrenByParent = new Map<string, FleetSession[]>();
  private readonly projectMembers = new Map<string, FleetSession[]>();
  private readonly shared: ReturnType<typeof overlapIndex>;

  constructor(private readonly data: FleetData) {
    for (const s of data.sessions) {
      this.sessions.set(sessionKey(s), s);
      const members = this.projectMembers.get(s.project) ?? [];
      members.push(s); this.projectMembers.set(s.project, members);
      if (s.parent) {
        const key = sessionKey({ machine: s.machine, id: s.parent });
        const children = this.childrenByParent.get(key) ?? [];
        children.push(s); this.childrenByParent.set(key, children);
      }
    }
    for (const children of this.childrenByParent.values()) children.sort(byState);
    this.groups = groupFleet(data);
    this.machines = sortedMachines(data.machines);
    this.projects = [...this.projectMembers.keys()].map(id => this.project(id) ?? { id, name: id, repo: "" }).sort((a, b) => a.name.localeCompare(b.name));
    this.shared = overlapIndex(data.sessions);
  }

  session(id: string, machine?: string): FleetSession | undefined {
    if (machine !== undefined) return this.sessions.get(sessionKey({ machine, id }));
    const matches = this.data.sessions.filter(s => s.id === id);
    return matches.length === 1 ? matches[0] : undefined;
  }
  machine(id: string): FleetMachine | undefined { return this.data.machines.find(m => m.id === id); }
  project(id: string): FleetProject | undefined { return this.data.projects.find(p => p.id === id); }
  projectSessions(id: string): FleetSession[] { return this.projectMembers.get(id) ?? []; }
  projectMachines(id: string): FleetMachine[] {
    const ids = new Set(this.projectSessions(id).map(s => s.machine));
    return this.machines.filter(m => ids.has(m.id));
  }
  parent(s: FleetSession): FleetSession | undefined { return s.parent ? this.session(s.parent, s.machine) : undefined; }
  children(s: FleetSession): FleetSession[] { return this.childrenByParent.get(sessionKey(s)) ?? []; }
  roots(machine: string, project: string): FleetSession[] { return this.projectSessions(project).filter(s => s.machine === machine && !this.parent(s)).sort(byState); }
  overlaps(s: FleetSession): { with: FleetSession; files: string[] }[] { return this.shared.get(this.session(s.id, s.machine)!) ?? []; }
  branch(root: FleetSession): { session: FleetSession; depth: number }[] {
    const result: { session: FleetSession; depth: number }[] = [];
    const walk = (s: FleetSession, depth: number, path: Set<string>) => {
      const key = sessionKey(s);
      if (path.has(key)) return;
      result.push({ session: s, depth });
      const next = new Set(path).add(key);
      for (const child of this.children(s)) walk(child, depth + 1, next);
    };
    walk(root, 0, new Set());
    return result;
  }
}

/** turnSummary says what the last turns held, for the tracker strip; a
 * session whose turns Driga does not know has a total of zero. */
export function turnSummary(session: FleetSession): { ok: number; waited: number; failed: number; total: number } {
  const last = (session.turns ?? []).slice(-30);
  return { ok: last.filter(t => t === "ok").length, waited: last.filter(t => t === "waited").length, failed: last.filter(t => t === "failed").length, total: last.length };
}

/** turnCells is the last 30 turns as tracker cells: a turn that went
 * through is quiet, one that waited is yellow, one that failed is red, and
 * cells before the first turn are empty. */
export function turnCells(session: FleetSession): TrackerCell[] {
  const last = (session.turns ?? []).slice(-30);
  const pad = 30 - last.length;
  const state: Record<string, State> = { ok: "working", waited: "waiting", failed: "stuck" };
  return Array.from({ length: 30 }, (_, i) => i < pad
    ? { state: null, label: "before the first turn" }
    : { state: state[last[i - pad]], label: `turn ${i - pad + 1} of ${last.length} · ${last[i - pad] === "ok" ? "went through" : last[i - pad] === "waited" ? "waited on you" : "failed"}` });
}

/** turnWords says the summary the strip is named by: "26 went through, 0 waited, 4 failed". */
export function turnWords(session: FleetSession): string {
  const { ok, waited, failed } = turnSummary(session);
  return `${ok} went through, ${waited} waited, ${failed} failed`;
}

/** sessionCaption is the one mono line under a session's title:
 * agent, branch and memory. */
export function sessionCaption(s: FleetSession): string {
  return [agentLabel(s.agent), s.branch, bytes(s.memory)].filter(Boolean).join(" · ");
}

/** machineCard is a machine group as the machine card takes it: counts of
 * the live states, and the strip of past polls when the app has kept any. */
export function machineCard(group: FleetMachineGroup, strip?: Machine["strip"]): Machine {
  const { machine: m, counts: c } = group;
  return {
    id: m.id, name: m.name, kind: m.kind, local: m.local, reachable: m.reachable, lastSeen: m.lastSeen,
    memUsed: m.memoryUsed, memTotal: m.memoryTotal, cpuPercent: m.cpuPercent, diskFree: m.diskFree, diskTotal: m.diskTotal,
    sessions: { working: c.working, waiting: c.waiting, stuck: c.stuck, idle: c.idle },
    strip,
    leftover: { trees: m.leftover?.trees ?? m.leftover?.processes ?? 0, bytes: m.leftover?.memory ?? 0 },
  };
}

/** reach says how a machine is reached: "reachable" or when it was last seen. */
export function reach(m: FleetMachine): string {
  return m.reachable ? "reachable" : `unreachable · last seen ${relative(m.lastSeen)}`;
}
