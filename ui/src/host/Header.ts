// The header's contract: what it is told and what it reports back, and the
// mapping from what the app already loads (the header fragment, the fleet,
// and the history the service persists) onto it.
import { Eye, HandWaving, Hourglass, type Icon } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import type { FleetData } from "../fleet/FleetTypes";
import { bytes, relative } from "../panels/format";
import type { Fragment, Panel } from "../panels/types";
import type { Machine as MachineCard } from "../widgets/MachineCard";
import { behaviorOf, type TileBehavior } from "./headerTiles";
import { liveStates, type LiveState } from "../widgets/types";

/** The attention shortcut order, independent of the stored tile order. */
export type AttentionKind = "waiting" | "review" | "stuck";
export const attentionKinds: readonly AttentionKind[] = ["waiting", "review", "stuck"];

/** The panels the header can point Home at. */
export type HeaderTarget = "inbox" | "stuck" | "leftover" | "reclaim" | "memory";

/** What each attention count is called, the tone it takes above zero, its
 * icon, the key that filters the inbox, what filtering by it means, and the
 * Home panel it is about. */
export const kinds: Record<AttentionKind, { label: string; tone: "warning" | "info" | "danger"; icon: Icon; key: string; hint: string; target: HeaderTarget }> = {
  waiting: { label: "Need you", tone: "warning", icon: HandWaving, key: "1", hint: "Filter the inbox to what needs you", target: "inbox" },
  review: { label: "To review", tone: "info", icon: Eye, key: "2", hint: "Filter the inbox to what finished", target: "inbox" },
  stuck: { label: "Stuck", tone: "danger", icon: Hourglass, key: "3", hint: "Show the stuck sessions on Home", target: "stuck" },
};

/** One attention count: its value now, its readings over the last hour
 * and its change over that hour. */
export type Attention = { count: number; trend: (number | null)[]; delta: number };

/** One poll of a machine: its worst session state then, quiet when it ran
 * none, offline when Driga could not reach it. */
export type Poll = LiveState | "quiet" | "offline";

export type Machine = {
  id: string;
  name: string;
  /** The machine the app runs on. */
  local?: boolean;
  reachable: boolean;
  sessions: Record<LiveState, number>;
  memory: { used: number; total: number };
  /** The last polls, oldest first, up to 30. */
  polls: Poll[];
  seenAt: string;
};

export type HeaderTile = {
  id: string; title: string; viz: "stat" | "gauge"; panel: Panel;
  value: number | null; unit?: string; total?: number | null; caption?: number | null;
  trend?: (number | null)[]; max?: number; error?: string; behavior: TileBehavior;
};

export type HeaderData = {
  tiles: HeaderTile[];
  localMachine: string;
  machines: Machine[];
  checkedAt: string;
};

export type HeaderProps = {
  /** Nothing until the first load; the header then shows its mark, search and Chat only. */
  data: HeaderData | null;
  /** The attention kind the inbox is filtered to, if any. */
  filter?: AttentionKind | null;
  chatOpen: boolean;
  onFilter: (kind: AttentionKind) => void;
  onOpenFleet: () => void;
  onSearch: () => void;
  onToggleChat: () => void;
  /** A tile asks for its panel on Home. */
  onShow: (panel: Panel) => void;
  /** A tile asks Chat about itself. */
  onAsk?: (about: string) => void;
  /** The app's search control, in place of the header's own button. */
  search?: ReactNode;
  editor?: ReactNode;
  busy?: boolean;
  onRemove?: (id: string) => void;
  onMove?: (id: string, before?: string) => void;
};

const severity: readonly LiveState[] = ["stuck", "waiting", "working", "idle"];

/** worst is the machine's most pressing session state, or null when it runs none. */
export function worst(m: { sessions: Record<LiveState, number> }): LiveState | null {
  return severity.find(s => m.sessions[s] > 0) ?? null;
}

export function percent(used: number, total: number): number {
  return total > 0 ? Math.round((used / total) * 100) : 0;
}

/** machineDetail is what a machine's name does not say: whether it is this
 * machine, its sessions by state or that it is unreachable, and when it was
 * last seen: this machine · 5 working · 1 waiting · seen 4s ago. */
export function machineDetail(m: Machine, now: number): string {
  const states = severity.filter(s => m.sessions[s] > 0).map(s => `${m.sessions[s]} ${s}`);
  const health = m.reachable ? (states.length ? states : ["no sessions"]) : ["unreachable"];
  return [...(m.local ? ["this machine"] : []), ...health, `seen ${relative(m.seenAt, now)}`].join(" · ");
}

/** machineSummary is one line about a machine, for its tooltip:
 * studio · this machine · 5 working · 1 waiting · 24 GB of 32 GB · seen 4s ago. */
export function machineSummary(m: Machine, now: number): string {
  return [m.name, machineDetail(m, now), `${bytes(m.memory.used)} of ${bytes(m.memory.total)}`].join(" · ");
}

/** fleetNotes names every machine that is stuck, waiting or unreachable,
 * or says that none is. */
export function fleetNotes(machines: Machine[]): string {
  const notes = machines.flatMap(m => !m.reachable ? [`${m.name} unreachable`] : worst(m) === "stuck" ? [`${m.name} stuck`] : worst(m) === "waiting" ? [`${m.name} waiting`] : []);
  return notes.length ? notes.join(" · ") : "all working or idle";
}

/** fleetSummary is the fleet's size and its notes, for the fleet control's tooltip. */
export function fleetSummary(machines: Machine[]): string {
  return `Fleet: ${machines.length} machine${machines.length === 1 ? "" : "s"} · ${fleetNotes(machines)}`;
}

/** changeWords says an attention count's hour in words: "up 2 in the last hour". */
export function changeWords(a: Attention): string {
  if (a.trend.length < 2 || a.trend[0] === null || a.trend[a.trend.length - 1] === null) return "not enough history for an hour’s change";
  return a.delta === 0 ? "steady over the last hour" : `${a.delta > 0 ? "up" : "down"} ${Math.abs(a.delta)} in the last hour`;
}

/** tilesOf projects only the stored panels, in their stored order. */
export function tilesOf(header: Fragment): HeaderTile[] {
  const results = new Map(header.results.map(r => [r.id, r]));
  const number = (n: unknown) => typeof n === "number" && Number.isFinite(n) ? n : null;
  return header.dashboard.panels.filter(p => p.viz === "stat" || p.viz === "gauge").map(p => {
    const r = results.get(p.id);
    const ok = r?.status === "ok";
    return { id: p.id, title: r?.title || p.title, viz: p.viz as HeaderTile["viz"], panel: p,
      value: ok ? number(r.frame?.rows[0]?.[0]) : null, unit: p.unit ?? r?.frame?.columns[0]?.unit,
      total: ok ? number(r.total) : null, caption: ok && p.caption ? number(r.caption) : null,
      trend: ok ? r.trend : undefined, max: p.max, error: r?.status === "error" ? r.message ?? "This panel could not run." : undefined, behavior: behaviorOf(p) };
  });
}

/** History served by the service: each attention count's last hour and each machine's last polls. */
export type Kept = { attention: Record<AttentionKind, (number | null)[]>; polls: Record<string, Poll[]> };

export const nothingKept: Kept = { attention: { waiting: [], review: [], stuck: [] }, polls: {} };

/** machineOf reads one fleet machine with its sessions by state. */
export function machinesOf(fleet: FleetData, polls: Kept["polls"]): Machine[] {
  return fleet.machines.map(m => {
    const sessions = Object.fromEntries(liveStates.map(s => [s, fleet.sessions.filter(x => x.machine === m.id && x.state === s).length])) as Record<LiveState, number>;
    return { id: m.id, name: m.name, local: m.local, reachable: m.reachable, sessions, memory: { used: m.memoryUsed, total: m.memoryTotal }, polls: polls[m.id] ?? [], seenAt: m.lastSeen };
  });
}

/** pollOf is a machine's worst state now, for the service's polls. */
export function pollOf(m: Machine): Poll {
  return m.reachable ? worst(m) ?? "quiet" : "offline";
}

/** headerOf combines the spec projection with fleet names and durable history. */
export function headerOf(header: Fragment, fleet: FleetData | null, kept: Kept): HeaderData {
  const machines = fleet ? machinesOf(fleet, kept.polls) : [];
  return {
    tiles: tilesOf(header).map(tile => tile.behavior.attention ? { ...tile, trend: kept.attention[tile.behavior.attention] } : tile),
    localMachine: machines.find(m => m.local || m.id === header.local_machine)?.name ?? header.local_machine ?? "this machine",
    machines, checkedAt: header.observed_at,
  };
}

/** stripsOf turns the served polls into the machine cards' strips, one cell a poll. */
export function stripsOf(kept: Kept, machines: Machine[]): Record<string, MachineCard["strip"]> {
  return Object.fromEntries(machines.filter(m => (kept.polls[m.id]?.length ?? 0) > 1).map(m => {
    const polls = kept.polls[m.id];
    const cells = polls.map((p, i) => ({ state: p === "quiet" ? null : p, label: `poll ${i + 1} of ${polls.length} · ${p === "offline" ? "unreachable" : p === "quiet" ? "no sessions" : p}` }));
    return [m.id, { cells, from: `${polls.length} polls`, to: "now", label: `${m.name}, last ${polls.length} polls` }];
  }));
}
