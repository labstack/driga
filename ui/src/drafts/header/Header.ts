// The contract both header drafts implement: what the header is told and
// what it reports back. The app's container maps the header fragment, the
// fleet and the local machine's readings onto it.
import { Eye, HandWaving, Hourglass, type Icon } from "@phosphor-icons/react";
import { bytes, relative } from "../../panels/format";

/** The three counts that mean someone must act, in the order the header shows them. */
export type AttentionKind = "waiting" | "review" | "stuck";
export const attentionKinds: readonly AttentionKind[] = ["waiting", "review", "stuck"];

/** What each attention count is called, the tone it takes above zero, its
 * icon, the key that focuses its tile and what filtering by it means. */
export const kinds: Record<AttentionKind, { label: string; tone: "warning" | "info" | "danger"; icon: Icon; key: string; hint: string }> = {
  waiting: { label: "Need you", tone: "warning", icon: HandWaving, key: "1", hint: "Filter the inbox to what needs you" },
  review: { label: "To review", tone: "info", icon: Eye, key: "2", hint: "Filter the inbox to what finished" },
  stuck: { label: "Stuck", tone: "danger", icon: Hourglass, key: "3", hint: "Filter the inbox to stuck sessions" },
};

/** One attention count: its value now, a reading a minute over the last
 * hour and its change over that hour. */
export type Attention = { count: number; trend: number[]; delta: number };

/** A session's state: green, yellow, red and blue mean working, waiting,
 * stuck and idle on every surface Driga draws. */
export type SessionState = "working" | "waiting" | "stuck" | "idle";

/** One poll of a machine: its worst session state then, quiet when it ran
 * none, offline when Driga could not reach it. */
export type Poll = SessionState | "quiet" | "offline";

export type Machine = {
  name: string;
  /** The machine the app runs on. */
  local?: boolean;
  reachable: boolean;
  sessions: Record<SessionState, number>;
  memory: { used: number; total: number };
  /** The last 30 polls, oldest first. */
  polls: Poll[];
  seenAt: string;
};

/** The local machine's memory: used of total now, a reading a minute over
 * the last hour and the change over that hour, all in bytes. */
export type LocalMemory = { machine: string; used: number; total: number; trend: number[]; delta: number };

/** Chores rather than alarms: leftover process trees and what they hold,
 * and the bytes a cleanup would reclaim. */
export type Hygiene = { leftover: { count: number; bytes: number }; reclaimable: number };

export type HeaderProps = {
  attention: Record<AttentionKind, Attention>;
  hygiene: Hygiene;
  memory: LocalMemory;
  machines: Machine[];
  /** When the counts were last checked. */
  checkedAt: string;
  /** The attention kind the inbox is filtered to, if any. */
  filter?: AttentionKind | null;
  chatOpen: boolean;
  onFilter: (kind: AttentionKind) => void;
  onOpenFleet: () => void;
  onSearch: () => void;
  onToggleChat: () => void;
  /** A hygiene or memory tile asks for its panel on Home. */
  onShow: (panel: "leftover" | "reclaim" | "memory") => void;
};

/** The tone each session state draws in. */
export const stateTone: Record<SessionState, "success" | "warning" | "danger" | "info"> = { working: "success", waiting: "warning", stuck: "danger", idle: "info" };

const severity: readonly SessionState[] = ["stuck", "waiting", "working", "idle"];

/** worst is the machine's most pressing session state, or null when it runs none. */
export function worst(m: Machine): SessionState | null {
  return severity.find(s => m.sessions[s] > 0) ?? null;
}

export function percent(used: number, total: number): number {
  return total > 0 ? Math.round((used / total) * 100) : 0;
}

/** pressure is the color a memory bar takes: the accent until 70%, then
 * the waiting tone, then the stuck tone past 90%. */
export function pressure(pct: number): "brand" | "warning" | "danger" {
  return pct > 90 ? "danger" : pct >= 70 ? "warning" : "brand";
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

export type Polarity = "good" | "bad" | "neutral";

/** polarity says whether a change is good news. More to review is neutral;
 * more waiting, more stuck and more memory used are bad; a memory change
 * under 2% of the machine is noise. */
export function polarity(kind: AttentionKind | "memory", delta: number, total?: number): Polarity {
  if (kind === "review" || delta === 0) return "neutral";
  if (kind === "memory" && total && Math.abs(delta) / total < 0.02) return "neutral";
  return delta > 0 ? "bad" : "good";
}

const hhmm = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });

/** clock is a time as HH:MM, local. */
export function clock(iso: string): string {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? "–" : hhmm.format(t);
}
