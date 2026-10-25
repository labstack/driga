import { normalize, type Dashboard, type Fragment, type Panel, type Prompt } from "../panels/types";

async function post<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), signal });
  if (!response.ok) throw new Error((await response.text()).trim() || `${response.status}`);
  return response.json() as Promise<T>;
}

async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal });
  if (!response.ok) throw new Error((await response.text()).trim() || `${response.status}`);
  return response.json() as Promise<T>;
}

export type Summary = { id: string; name: string; panels: number; version: number };

export type Operation =
  | { op: "add"; panel: Panel; before?: string }
  | { op: "update"; id: string; panel: Partial<Panel> }
  | { op: "remove"; id: string }
  | { op: "layout"; grids: Record<string, { x: number; y: number; w: number; h: number }> }
  | { op: "rename"; name: string };

export const dashboards = {
  list: () => post<{ dashboards: Summary[] }>("/app/api/list", {}).then(r => r.dashboards),
  open: (id: string, vars?: Record<string, string>, signal?: AbortSignal) => post<Fragment>("/app/api/open", { id, vars }, signal).then(normalize),
  run: (dashboard: Dashboard, vars?: Record<string, string>) => post<Fragment>("/app/api/run", { dashboard, vars }).then(normalize),
  edit: (id: string, operations: Operation[], base: number, vars?: Record<string, string>, message?: string) =>
    post<Fragment>("/app/api/edit", { id, operations, base_version: base, vars, message }).then(normalize),
};

/** confirm asks the app's native dialog; without one nothing is confirmed. */
export async function confirm(message: string): Promise<boolean> {
  try { return (await post<{ confirmed: boolean }>("/app/confirm", { message })).confirmed === true; } catch { return false; }
}

/** action runs a local action through the app's checks. */
export function action(body: Record<string, unknown>): Promise<unknown> {
  return post("/app/action", body);
}

// Chat runs through the collector's broker: one conversation, long-polled.
export type ChatMessage = { id: string; role: string; text: string; status: string; html?: string; created_at: string };
export type ChatEvent = { seq: number; kind: string; message?: ChatMessage; message_id?: string; text?: string; action?: ChatAction };
export type ChatAction = { id: string; tool: string; title: string; status: string; result?: string };
export type ChatState = { conversation: { id: string }; messages: ChatMessage[]; events: ChatEvent[]; actions: ChatAction[]; cursor: number; reset: boolean; active?: boolean; mode: string };

export const chat = {
  history: () => get<{ id: string }[]>("/chat/api/history"),
  create: () => post<{ id: string }>("/chat/api/new", {}),
  send: (id: string, text: string, context: Record<string, unknown>) => post("/chat/api/send", { id, text, context }),
  stop: (id: string) => post("/chat/api/stop", { id }),
  decide: (id: string, action: string, allow: boolean) => post("/chat/api/decide", { id, action, allow }),
  prompts: () => get<Prompt[] | null>("/chat/api/prompts").then(p => p ?? []),
  answer: (id: string, allow: boolean) => post("/chat/api/prompt-answer", { id, allow }),
  wait: (id: string, after: number, signal: AbortSignal) => get<ChatState>(`/chat/api/wait?id=${encodeURIComponent(id)}&after=${after}`, signal),
};
