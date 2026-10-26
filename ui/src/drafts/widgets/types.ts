// The vocabulary the widget drafts share. A state is what a session is
// doing and carries one of the four meaning colors; an agent family is a
// category and carries a categorical tint, never a state color.

/** A session's state. finished shares idle's blue and differs by its icon. */
export type State = "working" | "waiting" | "stuck" | "idle" | "finished";

/** The agent families Driga supervises. */
export type Agent = "claude" | "codex" | "gemini" | "copilot" | "agy";

/** The fixed order states are listed in: what needs the person first. */
export const stateOrder: readonly State[] = ["waiting", "stuck", "working", "finished", "idle"];

/** The word a state shows beside its dot; never color alone. */
export const stateWord: Record<State, string> = { working: "working", waiting: "waiting", stuck: "stuck", idle: "idle", finished: "finished" };

export const agentName: Record<Agent, string> = { claude: "Claude Code", codex: "Codex", gemini: "Gemini", copilot: "Copilot", agy: "agy" };
