// The header's contract: what it is told and what it reports back, and the
// mapping from what the app already loads (the header fragment, the fleet,
// Chat's prompts and the history the service persists) onto it.
import { Eye, HandWaving, Hourglass, type Icon } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import type { FleetData } from "../fleet/FleetTypes";
import { bytes, relative } from "../panels/format";
import type { Prompt, Fragment } from "../panels/types";
import type { Polarity } from "../widgets/DeltaChip";
import type { Machine as MachineCard } from "../widgets/MachineCard";
import { liveStates, type LiveState } from "../widgets/types";

/** The three counts that mean someone must act, in the order the header shows them. */
export type AttentionKind = "waiting" | "review" | "stuck";
export const attentionKinds: readonly AttentionKind[] = ["waiting", "review", "stuck"];

/** The panels the header can point Home at. */
export type HeaderTarget = "inbox" | "stuck" | "leftover" | "reclaim" | "memory";

/** What each attention count is called, the tone it takes above zero, its
 * icon, the key that focuses its tile, what filtering by it means, and the
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

/** The local machine's memory: used of total now, its readings over the
 * last hour and the change over that hour, all in bytes. */
export type LocalMemory = { machine: string; used: number; total: number; trend: (number | null)[]; delta: number };

/** Chores rather than alarms: leftover process trees and what they hold,
 * and the bytes a cleanup would reclaim. The count comes directly from the service, independently of the
 * paginated Fleet tables. */
export type Hygiene = { leftover: { count?: number; bytes: number }; reclaimable: number };

export type HeaderData = {
  attention: Record<AttentionKind, Attention>;
  hygiene: Hygiene;
  memory: LocalMemory;
  machines: Machine[];
  /** When the counts were last checked. */
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
  onShow: (target: HeaderTarget) => void;
  /** A tile asks Chat about itself. */
  onAsk?: (about: string) => void;
  /** The app's search control, in place of the header's own button. */
  search?: ReactNode;
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

/** polarity says whether a change is good news. More to review is neutral;
 * more waiting, more stuck and more memory used are bad; a memory change
 * under 2% of the machine is noise. */
export function polarity(kind: AttentionKind | "memory", delta: number, total?: number): Polarity {
  if (kind === "review" || delta === 0) return "neutral";
  if (kind === "memory" && total && Math.abs(delta) / total < 0.02) return "neutral";
  return delta > 0 ? "bad" : "good";
}

/** changeWords says an attention count's hour in words: "up 2 in the last hour". */
export function changeWords(a: Attention): string {
  if (a.trend.length < 2) return "not enough history for an hour’s change";
  return a.delta === 0 ? "steady over the last hour" : `${a.delta > 0 ? "up" : "down"} ${Math.abs(a.delta)} in the last hour`;
}

const hhmm = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });

/** clock is a time as HH:MM, local. */
export function clock(iso: string): string {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? "–" : hhmm.format(t);
}

/** stat reads a stat panel's value from the header fragment. */
export function stat(fragment: Fragment, id: string): number | null {
  const v = fragment.results.find(r => r.id === id)?.frame?.rows[0]?.[0];
  return typeof v === "number" ? v : null;
}

/** History served by the service: each attention count's last hour and each machine's last polls. */
export type Kept = { attention: Record<AttentionKind, (number | null)[]>; polls: Record<string, Poll[]> };

export const nothingKept: Kept = { attention: { waiting: [], review: [], stuck: [] }, polls: {} };

/** counts reads the three attention counts the header fragment and Chat's
 * prompts give: waits plus prompts, finished turns, sessions that look stuck. */
export function counts(header: Fragment, prompts: Prompt[]): Record<AttentionKind, number> {
  const waiting = new Set(header.waiting_sessions);
  let extra = 0;
  for (const p of prompts) {
    if (!p.session_id || !waiting.has(p.session_id)) {
      extra++;
      if (p.session_id) waiting.add(p.session_id);
    }
  }
  return { waiting: (stat(header, "waiting") ?? 0) + extra, review: stat(header, "finished") ?? 0, stuck: stat(header, "stuck") ?? 0 };
}

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

/** headerOf maps what the app has onto the header: the fragment's counts
 * and memory, the fleet's machines, Chat's prompts, and the service history.
 * Without the fleet the header still shows its counts, with the local
 * machine named by its id and no dots. */
export function headerOf(header: Fragment, fleet: FleetData | null, prompts: Prompt[], kept: Kept): HeaderData {
  const now = counts(header, prompts);
  const attention = Object.fromEntries(attentionKinds.map(k => {
    const trend = kept.attention[k];
    return [k, { count: now[k], trend, delta: trend.length > 1 && trend[trend.length - 1] !== null && trend[0] !== null ? trend[trend.length - 1]! - trend[0]! : 0 }];
  })) as Record<AttentionKind, Attention>;
  const machines = fleet ? machinesOf(fleet, kept.polls) : [];
  const local = machines.find(m => m.local) ?? fleet?.machines.find(m => m.id === header.local_machine);
  const memory = header.results.find(r => r.id === "memory");
  const trend = memory?.trend ?? [];
  const known = trend.filter((n): n is number => typeof n === "number");
  return {
    attention,
    hygiene: { leftover: { count: header.leftover_count, bytes: stat(header, "leftover") ?? 0 }, reclaimable: stat(header, "reclaim") ?? 0 },
    memory: { machine: local?.name ?? header.local_machine ?? "this machine", used: stat(header, "memory") ?? 0, total: memory?.total ?? 0, trend, delta: known.length > 1 ? known[known.length - 1] - known[0] : 0 },
    machines,
    checkedAt: header.observed_at,
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
