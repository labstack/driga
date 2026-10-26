// The Fleet view's contract: what Driga knows about machines, projects and
// sessions, as a fleet API would send it, and the actions a view reports.
// Both variants take the same props, so the owner's choice costs no data work.
import type { StatusTone } from "../../panels/StatusBadge";

export type FleetState = "working" | "waiting" | "stalled" | "idle" | "finished";

export type FleetAgent = "claude" | "codex" | "gemini" | "copilot" | "agy";

/** One turn's outcome, oldest first; the tracker strip shows the last 30. */
export type FleetTurn = "ok" | "waited" | "failed";

export type FleetMachine = {
  id: string;
  name: string;
  kind: "desktop" | "laptop" | "server";
  /** The machine Driga runs on. */
  local?: boolean;
  reachable: boolean;
  /** When Driga last heard from it, ISO. */
  lastSeen: string;
  memoryUsed: number;
  memoryTotal: number;
  /** Agent processes no session owns, which Reclaim can end. */
  leftover?: { processes: number; memory: number };
};

export type FleetProject = { id: string; name: string; repo: string };

export type FleetSession = {
  id: string;
  title: string;
  state: FleetState;
  /** Read from process and file activity rather than the agent, or last known from an unreachable machine. */
  inferred?: boolean;
  agent: FleetAgent;
  project: string;
  machine: string;
  branch?: string;
  /** Resident memory, bytes. */
  memory: number;
  /** The session that spawned this one as a subagent. */
  parent?: string;
  /** Paths edited so far, relative to the repo. */
  files: string[];
  lastTool?: string;
  lastActivity: string;
  started: string;
  turns: FleetTurn[];
  /** Why it waits, when it waits: a permission, a question, a review. */
  waiting?: string;
};

export type FleetSelection = { kind: "machine" | "project" | "session"; id: string };

export type FleetData = { machines: FleetMachine[]; projects: FleetProject[]; sessions: FleetSession[] };

export type FleetActions = {
  onSelect: (selection: FleetSelection | null) => void;
  onOpen: (session: FleetSession) => void;
  onReply: (session: FleetSession) => void;
};

export type FleetProps = FleetData & FleetActions & {
  /** Controlled selection; defaultSelected starts an uncontrolled one. */
  selected?: FleetSelection | null;
  defaultSelected?: FleetSelection | null;
};

/** Each state's label, shared tone and rank: what needs the person sorts
 * first. Green, yellow, red and blue mean working, waiting, stuck and idle
 * on every surface Driga draws; finished is the one neutral. */
export const states: Record<FleetState, { label: string; tone: StatusTone; rank: number }> = {
  waiting: { label: "waiting", tone: "warning", rank: 0 },
  stalled: { label: "stalled?", tone: "danger", rank: 1 },
  working: { label: "working", tone: "success", rank: 2 },
  idle: { label: "idle", tone: "info", rank: 3 },
  finished: { label: "finished", tone: "neutral", rank: 4 },
};

export const stateOrder: FleetState[] = ["waiting", "stalled", "working", "idle", "finished"];

export const agents: Record<FleetAgent, string> = { claude: "Claude Code", codex: "Codex", gemini: "Gemini", copilot: "Copilot", agy: "agy" };

/** toneVar is a tone's CSS color, for DOM surfaces; neutral is the quiet
 * control border, so finished work never takes a status hue. */
export function toneVar(tone: StatusTone): string {
  return tone === "neutral" ? "var(--driga-control-border)" : `var(--mantine-color-${tone}-filled)`;
}
