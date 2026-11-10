// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Fragment } from "../panels/types";
import { PanelView } from "../panels/PanelView";
import { useHost } from "../panels/host";
import { App } from "./App";

const api = vi.hoisted(() => ({ open: vi.fn(), run: vi.fn(), list: vi.fn(), prompts: vi.fn(), bursts:vi.fn(), grantHistory:vi.fn(), edit: vi.fn(), history: vi.fn(), wait: vi.fn(), setMode: vi.fn() }));
vi.mock("./api", async importOriginal => ({
  ...await importOriginal<typeof import("./api")>(),
  dashboards: { open: api.open, run: api.run, list: api.list, edit: api.edit },
  chat: { history: api.history, wait: api.wait, setMode: api.setMode, bursts:api.bursts, grantHistory:api.grantHistory, prompts: api.prompts }, action: vi.fn(), confirmedAction: vi.fn(), confirm: vi.fn(),
}));
vi.mock("./ClustersHeader", () => ({ ClustersHeader: ({ onToggleChat, search, data }: { onToggleChat: () => void; search: ReactNode; data: { checkedAt: string } | null }) => <><button onClick={onToggleChat}>Toggle Chat</button><output data-testid="header">{data?.checkedAt}</output>{search}</> }));
vi.mock("./Search", () => ({ Search: ({ open }: { open: (id: string) => void }) => <><button onClick={() => open("other")}>Other dashboard</button><button onClick={() => open("home")}>Home dashboard</button></> }));
vi.mock("./Dashboard", () => ({ Dashboard: ({ fragment, onRemove }: { fragment: Fragment; onRemove: (id: string) => void }) => <><PanelView panel={{ id: "inbox", title: "Inbox", viz: "inbox" }} result={{ id: "inbox", status: "ok", frame: { columns: [{ name: "kind", type: "string" }, { name: "title", type: "string" }], rows: [["waiting", "Waiting row"], ["finished", "Review row"]] } }} /><output data-testid="filter">{useHost().inboxFilter}</output><div data-testid="dashboard">{fragment.id}</div><output data-testid="warning">{fragment.results[0]?.message}</output><button onClick={() => onRemove("panel")}>Remove panel</button></> }));
vi.mock("../fleet/FleetPage", () => ({ FleetPage: () => <div data-testid="fleet">Fleet</div> }));
vi.mock("react-resizable-panels", async () => {
  const { useRef } = await import("react");
  const Box = ({ children, id, inert }: { children: ReactNode; id?: string; inert?: boolean }) => <div id={id} inert={inert}>{children}</div>;
  return { Group: Box, Panel: Box, Separator: () => <div />, useDefaultLayout: () => ({}), usePanelRef: () => useRef({ isCollapsed: () => false, collapse: vi.fn(), expand: vi.fn() }) };
});

const fragment = (id: string): Fragment => ({ id, version: 1, dashboard: { name: id, panels: [] }, results: [], observed_at: "2026-03-14T15:00:00Z", history: { attention: [], polls: [] } });
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let root: Root;
let container: HTMLDivElement;
const click = async (label: string) => {
  const button = [...container.querySelectorAll("button")].find(b => b.textContent === label)!;
  await act(async () => { button.click(); });
};
const key = async (value: string) => { await act(async () => { document.dispatchEvent(new KeyboardEvent("keydown", { key: value, bubbles: true })); }); };
async function mount() { await act(async () => { root.render(<MantineProvider><App /></MantineProvider>); }); }

beforeEach(() => {
  vi.clearAllMocks();
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  history.replaceState(null, "", "/app/");
  localStorage.clear();
  api.open.mockImplementation((id: string) => Promise.resolve(fragment(id)));
  api.run.mockResolvedValue(fragment("run"));
  api.list.mockResolvedValue([]);
  api.history.mockResolvedValue([]);
  api.wait.mockImplementation(() => new Promise(() => {}));
  api.setMode.mockResolvedValue({});
  HTMLElement.prototype.scrollTo = vi.fn();
  Object.defineProperty(document, "fonts", { configurable: true, value: new EventTarget() });
  api.prompts.mockResolvedValue([]); api.bursts.mockResolvedValue([]); api.grantHistory.mockResolvedValue([]);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });

it("native Back returns from Fleet to the previous dashboard", async () => {
  await mount();
  await click("Other dashboard");
  await key("g"); await key("f");
  expect(container.querySelector('[data-testid="fleet"]')).not.toBeNull();
  await act(async () => {
    (window as unknown as { drigaCommand: (name: string) => void }).drigaCommand("back");
    await new Promise(resolve => setTimeout(resolve, 20));
  });
  expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("other");
});

it("closing full-auto Chat with the header returns to Confirm", async () => {
  api.history.mockResolvedValue([{id: "conversation"}]);
  api.wait.mockResolvedValueOnce({conversation: {id: "conversation", agent: "claude"}, messages: [], actions: [], events: [], cursor: 0, reset: true, active: false, mode: "full auto", until: "2099-01-01T00:00:00Z"});
  await mount();
  expect(container.textContent).toContain("Mode: Full auto");
  await click("Toggle Chat");
  expect(api.setMode).toHaveBeenCalledWith("conversation", "confirm");
});

it.each(["history", "state"])("cannot hide Chat while its %s is unresolved", async pending => {
  if (pending === "history") api.history.mockImplementation(() => new Promise(() => {}));
  else api.history.mockResolvedValue([{ id: "conversation" }]);
  await mount();
  await click("Toggle Chat");
  expect(container.querySelector("#chat")?.hasAttribute("inert")).toBe(false);
});

it("cannot hide Chat while Full auto is being enabled", async () => {
  api.history.mockResolvedValue([{ id: "conversation" }]);
  api.wait.mockResolvedValueOnce({ conversation: { id: "conversation", agent: "claude" }, messages: [], actions: [], events: [], cursor: 0, reset: true, active: false, mode: "confirm" });
  const enabling = deferred<void>();
  api.setMode.mockReturnValue(enabling.promise);
  await mount();
  await click("Full auto");
  await act(async () => { [...document.querySelectorAll("button")].find(button => button.textContent === "Enable Full auto")!.click(); });
  expect(api.setMode).toHaveBeenCalledWith("conversation", "full auto");
  await click("Toggle Chat");
  expect(container.querySelector("#chat")?.hasAttribute("inert")).toBe(false);
  await act(async () => enabling.resolve());
  await click("Toggle Chat");
  expect(api.setMode).toHaveBeenLastCalledWith("conversation", "confirm");
  expect(container.querySelector("#chat")?.hasAttribute("inert")).toBe(true);
});

it("keeps a saved dashboard selected during a transient refresh failure", async () => {
  await mount();
  await click("Other dashboard");
  expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("other");
  api.open.mockImplementation((id: string) => id === "other" ? Promise.reject(new Error("temporary service failure")) : Promise.resolve(fragment(id)));
  await act(async () => { (window as unknown as { drigaCommand: (name: string) => void }).drigaCommand("reload"); });
  expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("other");
  expect(location.search).toBe("?dashboard=other");
});

it("Back passes a deleted dashboard without recreating its history entry", async () => {
  history.replaceState(null, "", "/app/?dashboard=previous");
  await mount();
  await click("Other dashboard");
  await click("Home dashboard");
  const { APIError } = await import("./api");
  api.open.mockImplementation((id: string) => id === "other" ? Promise.reject(new APIError("missing", 404)) : Promise.resolve(fragment(id)));
  await act(async () => { history.back(); await new Promise(resolve => setTimeout(resolve, 40)); });
  expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("home");
  await act(async () => { history.back(); await new Promise(resolve => setTimeout(resolve, 40)); });
  expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("previous");
});
