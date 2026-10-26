// The vocabulary the widgets, the header and the Fleet share. A state is
// what a session is doing and carries one of the four meaning colors; an
// agent family is a category and carries a categorical tint, never a state
// color.

/** A session's state. Green, yellow, red and blue mean working, waiting,
 * stuck and idle on every surface Driga draws; finished is neutral, with a
 * check, since a finished session needs nothing. */
export type State = "working" | "waiting" | "stuck" | "idle" | "finished";

/** The states a live session can be in. */
export type LiveState = Exclude<State, "finished">;

/** The agent families Driga supervises. */
export type Agent = "claude" | "codex" | "gemini" | "copilot" | "agy";

/** The fixed order states are listed in: what needs the person first. */
export const stateOrder: readonly State[] = ["waiting", "stuck", "working", "idle", "finished"];

export const liveStates: readonly LiveState[] = ["working", "waiting", "stuck", "idle"];

/** The word a state shows beside its dot; never color alone. */
export const stateWord: Record<State, string> = { working: "working", waiting: "waiting", stuck: "stuck", idle: "idle", finished: "finished" };

export const agentName: Record<Agent, string> = { claude: "Claude Code", codex: "Codex", gemini: "Gemini", copilot: "Copilot", agy: "agy" };

/** agentOf reads the family of an adapter name the service sends, if it is
 * one Driga knows; an unknown family draws as neutral. */
export function agentOf(name: string | undefined): Agent | undefined {
  return name && Object.hasOwn(agentName, name) ? name as Agent : undefined;
}

/** agentLabel names an agent family, or repeats an unknown adapter's name. */
export function agentLabel(name: string | undefined): string {
  const agent = agentOf(name);
  return agent ? agentName[agent] : name ?? "";
}
