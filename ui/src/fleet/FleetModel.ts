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

export type FleetOverlap = { a: string; b: string; files: string[] };

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
export function groupFleet({ machines, projects, sessions }: FleetData): FleetMachineGroup[] {
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

/** childrenOf lists the subagents a session spawned, in state order. */
export function childrenOf(sessions: FleetSession[], id: string): FleetSession[] {
  return sessions.filter(s => s.parent === id).sort(byState);
}

/** roots keeps the sessions that no other session spawned, so a tree shows
 * each subagent once, under its parent. */
export function roots(sessions: FleetSession[]): FleetSession[] {
  const ids = new Set(sessions.map(s => s.id));
  return sessions.filter(s => !s.parent || !ids.has(s.parent));
}

/** overlaps finds pairs of sessions of one project editing the same file,
 * each pair once, with the files they share. */
export function overlapIndex(sessions: FleetSession[]): Map<FleetSession, { with: FleetSession; files: string[] }[]> {
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

export function overlaps(sessions: FleetSession[]): FleetOverlap[] {
  const index = overlapIndex(sessions);
  const seen = new Set<FleetSession>();
  return sessions.flatMap(s => {
    seen.add(s);
    return index.get(s)!.filter(o => !seen.has(o.with)).map(o => ({a:s.id,b:o.with.id,files:o.files}));
  });
}

export function overlapsOf(sessions: FleetSession[], id: string, machine?: string): { with: FleetSession; files: string[] }[] {
  const own = sessions.find(s => s.id === id && (machine === undefined || s.machine === machine));
  return own ? overlapIndex(sessions).get(own)! : [];
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

export function machineOf(machines: FleetMachine[], id: string): FleetMachine | undefined {
  return machines.find(m => m.id === id);
}

export function projectOf(projects: FleetProject[], id: string): FleetProject | undefined {
  return projects.find(p => p.id === id);
}

/** machinesOfProject names the machines a project runs on, which answers
 * "the same repo on two machines" in words. */
export function machinesOfProject(data: FleetData, project: string): FleetMachine[] {
  const ids = new Set(data.sessions.filter(s => s.project === project).map(s => s.machine));
  return sortedMachines(data.machines.filter(m => ids.has(m.id)));
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
