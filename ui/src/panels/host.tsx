import { createContext, useContext } from "react";
import type { Prompt, Row } from "./types";

/** An action a panel row offers. The host decides how to run it: the app
 * asks its own dialog for anything that changes the machine and runs it
 * through its checks. A chat card offers none. */
export type ActionKind = "jump" | "ack" | "open" | "stop" | "kill" | "force_kill" | "cleanup" | "summarize";

export type Action = { kind: ActionKind; row: Row };

export type Act = (action: Action) => Promise<void>;

export type Host = {
  act?: Act;
  /** local is the machine the app runs on; actions are offered only there. */
  local?: string;
  prompts?: Prompt[];
  answer?: (prompt: Prompt, allow: boolean) => Promise<void>;
};

export const HostContext = createContext<Host>({});

export function useHost(): Host { return useContext(HostContext); }

const label: Record<ActionKind, string> = { jump: "Open pane", ack: "Ack", open: "Open", stop: "Stop", kill: "Kill", force_kill: "Force kill", cleanup: "Remove", summarize: "Summarize" };

export function actionLabel(kind: ActionKind): string { return label[kind]; }

/** rowActions lists what a row of a source can do, mirroring the TUI: only
 * local rows act, only verified idle trees are killed, only ready cleanup
 * candidates are removed. The app checks again before it acts. */
export function rowActions(source: string | undefined, row: Row, local?: string): ActionKind[] {
  const here = !row.machine_id || row.machine_id === local;
  switch (source) {
    case "orphans": return here && row.verified === true && row.young !== true ? ["kill", "force_kill"] : [];
    case "cleanup": return row.status === "ready" ? ["cleanup"] : [];
    case "sessions":
    case "flow": return here && row.label !== "ended" && row.label !== "dead" ? ["jump", "stop"] : [];
    case "projects": return here && row.path ? ["summarize"] : [];
    default: return [];
  }
}
