// The Fleet view's contract: what Driga knows about machines, projects and
// sessions, as the service sends it, and the actions the view reports. What
// the service does not send yet is optional, and the view draws it absent.
import { stateTone, type StatusTone } from "../widgets/StatusBadge";
import { stateOrder, type State } from "../widgets/types";

export type FleetState = State;

/** One turn's outcome, oldest first; the tracker strip shows the last 30. */
export type FleetTurn = "ok" | "waited" | "failed";

export type FleetMachine = {
  id: string;
  name: string;
  kind?: "desktop" | "laptop" | "server";
  /** The machine Driga runs on. */
  local?: boolean;
  reachable: boolean;
  /** When Driga last heard from it, ISO. */
  lastSeen: string;
  cpuPercent?: number;
  diskFree?: number;
  diskTotal?: number;
  memoryUsed: number;
  memoryTotal: number;
  /** Agent processes no session owns, which Reclaim can end: how many
   * trees, how many processes in them, and what they hold. */
  leftover?: { trees?: number; processes: number; memory: number };
  /** Agent memory on the machine split by what holds it. */
  memoryByRole?: { label: string; value: number }[];
};

export type FleetProject = { id: string; name: string; repo: string };

export type FleetSession = {
  id: string;
  title: string;
  state: FleetState;
  /** The service's own word when it is no state of ours: dead, unknown. */
  label?: string;
  /** Read from process and file activity rather than the agent, or last known from an unreachable machine. */
  inferred?: boolean;
  /** Why the state was inferred, in the service's words. */
  evidence?: string;
  /** The adapter name: claude, codex, gemini, copilot, agy. */
  agent: string;
  project: string;
  /** The machine's id. */
  machine: string;
  branch?: string;
  /** Resident memory, bytes. */
  memory: number;
  /** The session that spawned this one as a subagent. */
  parent?: string;
  /** Paths edited so far, relative to the repo. */
  files?: string[];
  lastTool?: string;
  lastActivity: string;
  started?: string;
  turns?: FleetTurn[];
  /** Why it waits, when it waits: a permission, a question, a review. */
  waiting?: string;
};

export type FleetSelection = { kind: "machine" | "project" | "session"; id: string; machine?: string };

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
  waiting: { label: "waiting", tone: stateTone.waiting, rank: 0 },
  stuck: { label: "stuck", tone: stateTone.stuck, rank: 1 },
  working: { label: "working", tone: stateTone.working, rank: 2 },
  idle: { label: "idle", tone: stateTone.idle, rank: 3 },
  finished: { label: "finished", tone: stateTone.finished, rank: 4 },
};

export { stateOrder };
