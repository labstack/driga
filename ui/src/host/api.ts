import { normalize, type Dashboard, type Fragment, type Panel, type Prompt, type PromptBurst } from "../panels/types";

export class APIError extends Error {
  constructor(message: string, readonly status: number) { super(message); }
}

async function post<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), signal });
  if (!response.ok) throw new APIError((await response.text()).trim() || `${response.status}`, response.status);
  return response.json() as Promise<T>;
}

async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal });
  if (!response.ok) throw new APIError((await response.text()).trim() || `${response.status}`, response.status);
  return response.json() as Promise<T>;
}

export type Summary = { id: string; name: string; panels: number; version: number };

export type Operation =
  | { op: "add"; panel: Panel; before?: string }
  | { op: "update"; id: string; panel?: Partial<Panel>; clear?: string[] }
  | { op: "remove"; id: string }
  | { op: "move"; id: string; before?: string }
  | { op: "layout"; grids: Record<string, { x: number; y: number; w: number; h: number }> }
  | { op: "rename"; name: string };

export const dashboards = {
  reset: (id: string, base: number) => post<Fragment>("/app/api/reset", { id, base_version: base }).then(normalize),
  catalog: () => post<{ widgets: Panel[] }>("/app/api/catalog", {}).then(r => r.widgets),
  replace: (id: string, dashboard: Dashboard, base: number, vars?: Record<string, string>, message?: string) =>
    post<Fragment>("/app/api/replace", { id, dashboard, base_version: base, vars, message }).then(normalize),
  list: () => post<{ dashboards: Summary[] }>("/app/api/list", {}).then(r => r.dashboards),
  open: (id: string, vars?: Record<string, string>, signal?: AbortSignal) => post<Fragment>("/app/api/open", { id, vars }, signal).then(normalize),
  run: (dashboard: Dashboard, vars?: Record<string, string>, signal?: AbortSignal) => post<Fragment>("/app/api/run", { dashboard, vars }, signal).then(normalize),
  create: (dashboard: Dashboard, message?: string) => post<Fragment>("/app/api/create", { dashboard, message }).then(normalize),
  delete: (id: string) => post<{ ok: boolean }>("/app/api/delete", { id }),
  edit: (id: string, operations: Operation[], base: number, vars?: Record<string, string>, message?: string) =>
    post<Fragment>("/app/api/edit", { id, operations, base_version: base, vars, message }).then(normalize),
};

/** Ask the native host; unavailable or malformed confirmations fail closed. */
async function confirmation(message: string, request?: Record<string, unknown>): Promise<{ confirmed: boolean; token?: string }> {
  try {
    const result = await post<{ confirmed: boolean; token?: string }>("/app/confirm", { message, request });
    return result.confirmed === true ? result : { confirmed: false };
  } catch { return { confirmed: false }; }
}
export async function confirm(message: string): Promise<boolean> {
  return (await confirmation(message)).confirmed;
}

/** Authorize and execute exactly one request with the host's one-use token. */
export async function confirmedAction(message: string, body: Record<string, unknown>): Promise<boolean> {
  const answer = await confirmation(message, body);
  if (!answer.confirmed || typeof answer.token !== "string" || answer.token.length === 0) return false;
  await action({ ...body, token: answer.token });
  return true;
}

/** action runs a local action through the app's checks. */
export function action(body: Record<string, unknown>): Promise<unknown> {
  return post("/app/action", body);
}

// Chat runs through the collector's broker: one conversation, long-polled.
export type ChatMessage = { id: string; role: string; text: string; status: string; html?: string; created_at: string };
export type ChatMode = "confirm" | "auto" | "full auto";
export type ChatEvent = { seq: number; kind: string; message?: ChatMessage; message_id?: string; text?: string; action?: ChatAction; mode?: ChatMode; until?: string };
export type ProcessRow = { pid: number; owner: string; started_at: string; executable: string; argv: string[]; command_line: string; memory_bytes: number; reason?: string };
export type CommandCard = { requested: { argv?: string[]; script?: string; cwd: string; timeout_seconds: number; network: boolean }; effective: string[][]; cwd: string; class: string; summary: string; boundary: string; error?: string; targets: ProcessRow[]; count: number; memory_bytes: number; force: boolean; stopped?: ProcessRow[]; vanished?: ProcessRow[]; skipped?: ProcessRow[]; survivors?: ProcessRow[] };
export type GrantDecision = { grant_id: string; action_id?: string; prompt_id?: string; conversation?: string; session?: string; time: string; attribution: string; revoked?: boolean };
export type ChatAction = { id: string; tool: string; title: string; input: unknown; directory: string; status: string; decided_by?: string; result?: string; command?: CommandCard; delegated?: Prompt; grant_id?: string; grant_eligible?: boolean; grant_pattern?: string; grant_expires_at?: string };
export type ChatState = { conversation: { id: string }; messages: ChatMessage[]; events: ChatEvent[]; actions: ChatAction[]; cursor: number; reset: boolean; active?: boolean; mode: ChatMode; until?: string };

export const chat = {
  history: () => get<{ id: string }[]>("/chat/api/history"),
  create: () => post<{ id: string }>("/chat/api/new", {}),
  send: (id: string, text: string, context: Record<string, unknown>) => post("/chat/api/send", { id, text, context }),
  stop: (id: string) => post("/chat/api/stop", { id }),
  decide: (id: string, action: string, allow: boolean) => post("/chat/api/decide", { id, action, allow }),
  decideSimilar: (id: string, action: string) => post("/chat/api/decide", { id, action, allow: true, similar: true }),
  answerSimilar: (id: string) => post("/chat/api/prompt-answer", { id, allow: true, similar: true }),
  revokeGrant: (id: string) => post("/chat/api/grant-revoke", { id }),
  grantHistory: () => get<GrantDecision[] | null>("/chat/api/grant-history").then(h => h ?? []),
  setMode: (id: string, mode: ChatMode) => post("/chat/api/mode", { id, mode }),
  bursts: () => get<PromptBurst[] | null>("/chat/api/prompt-bursts").then(b => b ?? []),
  decideBurst: (id: string, members: string[], allow: boolean) => post("/chat/api/burst-decide", { id, members, allow }),
  prompts: () => get<Prompt[] | null>("/chat/api/prompts").then(p => p ?? []),
  answer: (id: string, allow: boolean) => post("/chat/api/prompt-answer", { id, allow }),
  wait: (id: string, after: number, signal: AbortSignal) => get<ChatState>(`/chat/api/wait?id=${encodeURIComponent(id)}&after=${after}`, signal),
};
