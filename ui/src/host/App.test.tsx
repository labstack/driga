// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Fragment } from "../panels/types";
import { App } from "./App";

const api = vi.hoisted(() => ({ open: vi.fn(), run: vi.fn(), list: vi.fn(), prompts: vi.fn(), edit: vi.fn() }));
vi.mock("./api", () => ({
  dashboards: { open: api.open, run: api.run, list: api.list, edit: api.edit },
  chat: { prompts: api.prompts }, action: vi.fn(), confirmedAction: vi.fn(), confirm: vi.fn(),
}));
vi.mock("./ClustersHeader", () => ({ ClustersHeader: ({ onToggleChat, search }: { onToggleChat: () => void; search: ReactNode }) => <><button onClick={onToggleChat}>Toggle Chat</button>{search}</> }));
vi.mock("./Search", () => ({ Search: ({ open }: { open: (id: string) => void }) => <><button onClick={() => open("other")}>Other dashboard</button><button onClick={() => open("home")}>Home dashboard</button></> }));
vi.mock("./Dashboard", () => ({ Dashboard: ({ fragment }: { fragment: Fragment }) => <div data-testid="dashboard">{fragment.id}</div> }));
vi.mock("./Chat", () => ({ Chat: () => <button>Chat control</button> }));
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
  api.prompts.mockResolvedValue([]);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });

describe("App", () => {
  it("drops a previous dashboard response after a new dashboard opens", async () => {
    const old = deferred<Fragment>();
    const next = deferred<Fragment>();
    api.open.mockImplementation((id: string) => id === "home" ? old.promise : id === "other" ? next.promise : Promise.resolve(fragment(id)));
    await mount();
    await click("Other dashboard");
    expect(container.querySelector('[data-testid="dashboard"]')).toBeNull();
    await act(async () => { next.resolve(fragment("other")); });
    expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("other");
    await act(async () => { old.resolve(fragment("home")); });
    expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("other");
  });
  it("drops a previous dashboard error and clears the board while switching", async () => {
    const old = deferred<Fragment>();
    api.open.mockImplementation((id: string) => id === "other" ? old.promise : Promise.resolve(fragment(id)));
    await mount();
    expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("home");
    await click("Other dashboard");
    expect(container.querySelector('[data-testid="dashboard"]')).toBeNull();
    await click("Home dashboard");
    await act(async () => { old.reject(new Error("old request failed")); });
    expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("home");
    expect(container.textContent).not.toContain("Not updated since");
  });
  it("drops an older poll after the latest poll has completed", async () => {
    const old = deferred<Fragment>();
    const next = deferred<Fragment>();
    let calls = 0;
    api.open.mockImplementation((id: string) => id === "home" ? (++calls === 1 ? old.promise : next.promise) : Promise.resolve(fragment(id)));
    await mount();
    await act(async () => { (window as unknown as { drigaCommand: (name: string) => void }).drigaCommand("reload"); });
    await act(async () => { next.resolve(fragment("latest")); });
    await act(async () => { old.resolve(fragment("outdated")); });
    expect(container.querySelector('[data-testid="dashboard"]')?.textContent).toBe("latest");
  });
  it("makes collapsed Chat inert and restores its controls when opened", async () => {
    localStorage.setItem("driga-chat-open", "false");
    await mount();
    expect(container.querySelector("#chat")?.hasAttribute("inert")).toBe(true);
    expect(container.querySelector("#chat button")?.textContent).toBe("Chat control");
    await click("Toggle Chat");
    expect(container.querySelector("#chat")?.hasAttribute("inert")).toBe(false);
  });
  it("cancels g then f when another key intervenes", async () => {
    await mount();
    await key("g"); await key("x"); await key("f");
    expect(container.querySelector('[data-testid="fleet"]')).toBeNull();
    await key("g"); await key("f");
    expect(container.querySelector('[data-testid="fleet"]')).not.toBeNull();
  });
});
