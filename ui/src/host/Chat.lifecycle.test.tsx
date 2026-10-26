// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ChatAction, ChatMessage, ChatState } from "./api";
import { Chat } from "./Chat";

const api = vi.hoisted(() => ({ history: vi.fn(), create: vi.fn(), send: vi.fn(), wait: vi.fn(), decide: vi.fn(), stop: vi.fn() }));
vi.mock("./api", () => ({ chat: api }));
vi.mock("./ChatCard", () => ({ ChatCard: () => null }));
vi.mock("../panels/PanelView", () => ({ PanelView: () => null }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
type Wait = ReturnType<typeof deferred<ChatState>> & { id: string; after: number; signal: AbortSignal };
let waits: Wait[];
let container: HTMLDivElement;
let root: Root;
const message = (id: string, role: string, text: string, created_at = id): ChatMessage => ({ id, role, text, status: "complete", created_at });
const state = (messages: ChatMessage[] = [], actions: ChatAction[] = [], extra: Partial<ChatState> = {}): ChatState => ({ conversation: { id: "conversation" }, messages, actions, cursor: 1, reset: true, events: [], mode: "approve", ...extra });
async function mount(text?: string, at = 1) { await act(async () => { root.render(<MantineProvider><Chat ask={text ? { text, at } : null} onOpen={() => {}} /></MantineProvider>); }); }
async function click(label: string) {
  const button = [...container.querySelectorAll("button")].find(b => b.textContent === label)!;
  await act(async () => { button.click(); });
}
async function receive(response: ChatState) { const pending = waits[waits.length - 1]; await act(async () => pending.resolve(response)); }

beforeEach(() => {
  vi.clearAllMocks();
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  Object.defineProperty(document, "fonts", { configurable: true, value: new EventTarget() });
  waits = [];
  api.history.mockResolvedValue([]);
  api.create.mockResolvedValue({ id: "new-conversation" });
  api.send.mockResolvedValue({});
  api.decide.mockResolvedValue({});
  api.wait.mockImplementation((id: string, after: number, signal: AbortSignal) => {
    const next = { ...deferred<ChatState>(), id, after, signal };
    waits.push(next);
    signal.addEventListener("abort", () => next.reject(new DOMException("Aborted", "AbortError")), { once: true });
    return next.promise;
  });
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks(); });

describe("Chat lifecycle", () => {
  it("keeps a sent conversation when initial history arrives later", async () => {
    const history = deferred<{ id: string }[]>(); api.history.mockReturnValue(history.promise);
    await mount("new question"); await click("Send");
    expect(api.send).toHaveBeenCalledWith("new-conversation", "new question", { page: "/app/" });
    await act(async () => history.resolve([{ id: "old-conversation" }]));
    expect(waits.map(w => w.id)).toEqual(["new-conversation"]);
  });
  it("ignores initial history while creating a conversation for a send", async () => {
    const history = deferred<{ id: string }[]>(); const create = deferred<{ id: string }>();
    api.history.mockReturnValue(history.promise); api.create.mockReturnValue(create.promise);
    await mount("new question"); await click("Send");
    await act(async () => history.resolve([{ id: "old-conversation" }]));
    expect(waits).toHaveLength(0);
    await act(async () => create.resolve({ id: "new-conversation" }));
    expect(waits.map(w => w.id)).toEqual(["new-conversation"]);
    expect(api.send).toHaveBeenCalledWith("new-conversation", "new question", { page: "/app/" });
  });
  it("names Allow and Deny with the action title", async () => {
    api.history.mockResolvedValue([{ id: "conversation" }]); await mount();
    await receive(state([message("approval", "action", "")], [{ id: "approval", tool: "kill", title: "Stop session 7", status: "pending" }]));
    expect(container.querySelector('[aria-label="Allow Stop session 7"]')).not.toBeNull();
    expect(container.querySelector('[aria-label="Deny Stop session 7"]')).not.toBeNull();
  });
  it("blocks duplicate approval decisions until the service updates the action", async () => {
    api.history.mockResolvedValue([{ id: "conversation" }]);
    const decision = deferred<unknown>(); api.decide.mockReturnValue(decision.promise);
    await mount();
    await receive(state([message("approval", "action", "")], [{ id: "approval", tool: "kill", title: "Stop session 7", status: "pending" }]));
    const allow = [...container.querySelectorAll("button")].find(b => b.textContent === "Allow")!;
    const deny = [...container.querySelectorAll("button")].find(b => b.textContent === "Deny")!;
    await act(async () => { allow.click(); allow.click(); deny.click(); });
    expect(api.decide).toHaveBeenCalledTimes(1);
    expect(allow.disabled).toBe(true); expect(deny.disabled).toBe(true);
    await act(async () => decision.resolve({}));
    expect(allow.disabled).toBe(true); expect(deny.disabled).toBe(true);
  });
  it("catches an approval failure and allows a retry", async () => {
    api.history.mockResolvedValue([{ id: "conversation" }]);
    const decision = deferred<unknown>(); api.decide.mockReturnValue(decision.promise);
    await mount();
    await receive(state([message("approval", "action", "")], [{ id: "approval", tool: "kill", title: "Stop session 7", status: "pending" }]));
    await click("Allow");
    await act(async () => decision.reject(new Error("approval unavailable")));
    expect(container.querySelector('[role="alert"]')?.textContent).toBe("approval unavailable");
    expect([...container.querySelectorAll("button")].find(b => b.textContent === "Allow")?.disabled).toBe(false);
  });
  it("announces only the latest reply politely", async () => {
    api.history.mockResolvedValue([{ id: "conversation" }]); await mount();
    await receive(state([message("1", "assistant", "old answer"), message("2", "user", "new question"), message("3", "assistant", "latest answer")]));
    const live = [...container.querySelectorAll('[aria-live="polite"]')];
    expect(live).toHaveLength(1); expect(live[0].textContent).toBe("latest answer");
  });
  it("inserts new messages by creation time without sorting text deltas", async () => {
    api.history.mockResolvedValue([{ id: "conversation" }]); await mount();
    await receive(state([message("one", "user", "first question", "1"), message("three", "user", "third question", "3")]));
    await receive(state([], [], { reset: false, cursor: 2, events: [{ seq: 2, kind: "message", message: message("two", "user", "second question", "2") }] }));
    const compare = vi.spyOn(String.prototype, "localeCompare");
    await receive(state([], [], { reset: false, cursor: 3, events: [{ seq: 3, kind: "delta", message_id: "two", text: " appended" }] }));
    expect(compare).not.toHaveBeenCalled();
    expect(container.textContent!.indexOf("first question")).toBeLessThan(container.textContent!.indexOf("second question appended"));
    expect(container.textContent!.indexOf("second question appended")).toBeLessThan(container.textContent!.indexOf("third question"));
  });
  it("sends Enter during an active turn to the existing conversation queue", async () => {
    api.history.mockResolvedValue([{ id: "conversation" }]); await mount();
    await receive(state([], [], { active: true }));
    await mount("next question", 2);
    await act(async () => container.querySelector("textarea")!.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
    expect(api.send).toHaveBeenCalledWith("conversation", "next question", { page: "/app/" });
  });
});
