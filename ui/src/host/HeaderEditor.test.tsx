// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { Notifications, notifications } from "@mantine/notifications";
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { fragments } from "../fixtures/fragments";
import type { Dashboard, Fragment, Panel } from "../panels/types";
import { App } from "./App";
import { APIError, type Operation } from "./api";

const api = vi.hoisted(() => ({ open: vi.fn(), run: vi.fn(), list: vi.fn(), prompts: vi.fn(), edit: vi.fn(), replace: vi.fn(), catalog: vi.fn(), reset: vi.fn(), delete: vi.fn() }));
const size = vi.hoisted(() => ({ width: 10000 }));
vi.mock("./api", async importOriginal => ({ ...await importOriginal<typeof import("./api")>(), dashboards: api, chat: { prompts: api.prompts } }));
vi.mock("@mantine/hooks", async importOriginal => ({ ...await importOriginal<typeof import("@mantine/hooks")>(), useElementSize: (options?: unknown) => ({ ref: () => {}, width: options ? 288 : size.width, height: 56 }) }));
vi.mock("../panels/Chart", () => ({ Chart: () => null }));
vi.mock("./Search", () => ({ Search: () => <span /> }));
vi.mock("./Dashboard", () => ({ Dashboard: ({ fragment }: { fragment: Fragment }) => <div data-testid="board">{fragment.dashboard.panels.map(p => <h2 id={`panel-${p.id}`} key={p.id}>{p.title}</h2>)}</div> }));
vi.mock("./Chat", () => ({ Chat: ({ ask }: { ask: { text: string } | null }) => <output data-testid="draft">{ask?.text}</output> }));
vi.mock("react-resizable-panels", async () => {
  const { useRef } = await import("react");
  const Box = ({ children, id }: { children: ReactNode; id?: string }) => <div id={id}>{children}</div>;
  return { Group: Box, Panel: Box, Separator: () => null, useDefaultLayout: () => ({}), usePanelRef: () => useRef({ isCollapsed: () => false, collapse: vi.fn(), expand: vi.fn() }) };
});
const cpu: Panel = { id: "cpu", title: "CPU", viz: "gauge", source: "local_activity", measure: "max:cpu_percent", max: 100 };
const extra: Panel[] = [cpu, ...["disk", "load", "swap", "events", "agents", "working", "waiting_now", "inbox", "orphans", "machines_up", "agent_memory"].map(id => ({ id, title: id, viz: "stat" as const, source: "summary", measure: "sum:agents" }))];
let header: Fragment, home: Fragment, root: Root, container: HTMLDivElement;
const buttons = () => [...document.querySelectorAll<HTMLButtonElement>("button")];
async function click(label: string) {
  const b = buttons().find(b => b.textContent === label || b.getAttribute("aria-label") === label);
  expect(b, `button ${label}`).toBeTruthy();
  await act(async () => b!.click());
}
async function mount(portals = false) { await act(async () => root.render(<MantineProvider env={portals ? undefined : "test"}><Notifications /><App /></MantineProvider>)); }
async function pointerClick(label: string, hold = 0) {
  const button = buttons().find(b => b.textContent === label || b.getAttribute("aria-label") === label)!;
  expect(button, `button ${label}`).toBeTruthy();
  await act(async () => {
    button.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, pointerType: "mouse", button: 0 }));
    button.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, button: 0 }));
  });
  await act(async () => { await new Promise(r => setTimeout(r, hold)); });
  await act(async () => {
    button.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, pointerType: "mouse", button: 0 }));
    button.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, button: 0 }));
    button.click();
  });
}
async function removeReclaim() { await click("More for Reclaimable"); await click("Remove from header"); }
async function key(target: EventTarget, code: string) {
  await act(async () => { target.dispatchEvent(new KeyboardEvent("keydown", { key: code === "Space" ? " " : code, code, bubbles: true, cancelable: true })); await new Promise(r => setTimeout(r, 30)); });
}
beforeEach(() => {
  vi.resetAllMocks();
  size.width = 10000;
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  history.replaceState(null, "", "/app/"); localStorage.clear();
  header = { ...structuredClone(fragments.header), id: "header", version: 0 };
  home = { ...structuredClone(fragments.header), id: "home", version: 0, dashboard: { name: "Home", panels: [] }, results: [] };
  api.open.mockImplementation((id: string) => Promise.resolve(structuredClone(id === "header" ? header : home)));
  api.run.mockResolvedValue({ ...home, dashboard: { name: "Inbox", panels: [] } }); api.list.mockResolvedValue([]); api.prompts.mockResolvedValue([]);
  api.catalog.mockResolvedValue([...header.dashboard.panels, ...extra]);
  api.edit.mockImplementation(async (id: string, ops: Operation[], base: number) => {
    const current = id === "header" ? header : home;
    expect(base).toBe(current.version);
    const next = structuredClone(current); next.version = base + 1;
    for (const op of ops) {
      if (op.op === "remove") next.dashboard.panels = next.dashboard.panels.filter(p => p.id !== op.id);
      if (op.op === "add") next.dashboard.panels.push(op.panel);
    }
    if (id === "header") header = next; else home = next;
    return structuredClone(next);
  });
  api.replace.mockImplementation(async (_id: string, dashboard: Dashboard, base: number) => {
    expect(base).toBe(header.version);
    header = { ...header, dashboard, version: base + 1 }; return structuredClone(header);
  });
  api.reset.mockImplementation(async (_id: string, base: number) => { header = { ...structuredClone(fragments.header), id: "header", version: base + 1 }; return structuredClone(header); });
  api.delete.mockImplementation(async () => { header = { ...structuredClone(fragments.header), id: "header", version: 0 }; return { ok: true }; });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function(this: HTMLElement) {
    const tile = this.closest("[data-header-tile]");
    const index = tile ? [...tile.parentElement!.children].indexOf(tile) : 0;
    return { x: index * 300, y: 0, top: 0, left: index * 300, right: index * 300 + 288, bottom: 48, width: 288, height: 48, toJSON() {} };
  });
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => { notifications.clean(); root.unmount(); }); container.remove(); vi.restoreAllMocks(); });
it("remove from tile menu offers undo and immediately updates the header", async () => {
  await mount(); await removeReclaim();
  expect(api.edit).toHaveBeenCalledWith("header", [{ op: "remove", id: "reclaim" }], 0, undefined, expect.any(String));
  expect(buttons().some(b => b.textContent === "Undo")).toBe(true);
  expect(container.textContent).not.toContain("Reclaimable");
});
it("removes an overflow widget after a held pointer click with portals enabled", async () => {
  size.width = 1000;
  await mount(true);
  await pointerClick("+4");
  const overflow = document.querySelector('[role="menu"]')!;
  expect(overflow).not.toBeNull();
  expect(container.contains(overflow)).toBe(false);
  await pointerClick("More for Reclaimable");
  await pointerClick("Remove from header", 220);
  expect(api.edit).toHaveBeenCalledWith("header", [{ op: "remove", id: "reclaim" }], 0, undefined, expect.any(String));
  expect(header.dashboard.panels.some(p => p.id === "reclaim")).toBe(false);
});
it("undo replaces the first edited shipped header with its previous six panels", async () => {
  const previous = structuredClone(header.dashboard);
  await mount(); await removeReclaim(); await click("Undo");
  expect(api.replace).toHaveBeenCalledWith("header", previous, 1, undefined, expect.any(String));
  expect(header.dashboard.panels.map(p => p.id)).toEqual(["waiting", "finished", "stuck", "leftover", "reclaim", "memory"]);
  expect(container.textContent).toContain("Reclaimable");
});
it("picker lists only absent widgets; Enter adds CPU and Escape closes", async () => {
  await mount(); await click("Add widget");
  const options = [...document.querySelectorAll('[role="option"]')];
  expect(options).toHaveLength(12);
  expect(options.some(o => o.textContent === "Need you")).toBe(false);
  const search = document.querySelector<HTMLInputElement>('[aria-label="Find a widget"]')!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(search, "CPU");
    search.dispatchEvent(new Event("input", { bubbles: true }));
  });
  expect(document.querySelectorAll('[role="option"]')).toHaveLength(1);
  await key(search, "Enter");
  expect(api.edit).toHaveBeenCalledWith("header", [{ op: "add", panel: cpu }], 0, undefined, expect.any(String));
  await click("Add widget"); await key(document.querySelector('[aria-label="Find a widget"]')!, "Escape");
  expect(buttons().find(b => b.getAttribute("aria-label") === "Add widget")?.getAttribute("aria-expanded")).toBe("false");
});
it("last widget cannot be removed and explains why", async () => {
  header.dashboard.panels = [header.dashboard.panels[0]];
  await mount(); await click("More for Need you");
  const item = buttons().find(b => b.textContent === "Remove from header")!;
  expect(item.disabled).toBe(true);
  await act(async () => { item.parentElement!.dispatchEvent(new MouseEvent("mouseenter", { bubbles: false })); await new Promise(r => setTimeout(r, 350)); });
  expect(document.querySelector('[role="tooltip"]')?.textContent).toBe("The header needs one widget");
  expect(api.edit).not.toHaveBeenCalled();
});
it("conflict reloads the header without retry", async () => {
  api.edit.mockRejectedValue(new APIError("stale", 409));
  await mount(); const opens = api.open.mock.calls.filter(c => c[0] === "header").length;
  await removeReclaim();
  expect(document.body.textContent).toContain("Header changed elsewhere; reloaded");
  expect(api.open.mock.calls.filter(c => c[0] === "header")).toHaveLength(opens + 1);
  expect(api.edit).toHaveBeenCalledTimes(1);
});
it("keyboard drag moves memory before waiting in one edit", async () => {
  await mount();
  const handle = document.querySelector<HTMLElement>('[aria-label="Reorder Memory"]'); expect(handle).toBeTruthy();
  handle!.focus(); await key(handle!, "Space");
  for (let i = 0; i < 5; i++) await key(document, "ArrowLeft");
  await key(document, "Space");
  expect(api.edit).toHaveBeenCalledTimes(1);
  expect(api.edit).toHaveBeenCalledWith("header", [{ op: "move", id: "memory", before: "waiting" }], 0, undefined, expect.any(String));
});
it("Ask Chat for another opens Chat with a starter draft", async () => {
  await mount(); await click("Add widget"); await click("Ask Chat for another…");
  expect(container.querySelector('[data-testid="draft"]')?.textContent).toBe("Add a header widget that shows ");
});
it("reset saves a new version without deleting and undo restores the previous spec", async () => {
  header.dashboard.panels = [cpu]; const previous = structuredClone(header.dashboard);
  await mount(); await click("Header options"); await click("Reset header");
  expect(api.reset).toHaveBeenCalledWith("header", 0);
  expect(api.delete).not.toHaveBeenCalled();
  expect(container.textContent).toContain("Need you");
  await click("Undo"); expect(api.replace).toHaveBeenCalledWith("header", previous, 1, undefined, expect.any(String));
});
it("Show on Home opens a matching source and measure", async () => {
  home.dashboard.panels = [{ ...header.dashboard.panels[4], id: "existing", title: "Existing reclaim" }];
  await mount();
  const scroll = vi.spyOn(document.getElementById("panel-existing")!, "scrollIntoView").mockImplementation(() => {});
  await click("More for Reclaimable"); await click("Show on Home");
  await act(async () => { await new Promise(r => setTimeout(r, 30)); });
  expect(scroll).toHaveBeenCalledOnce();
  expect(api.edit).not.toHaveBeenCalled();
  expect(container.querySelector('[data-testid="board"]')?.textContent).toContain("Existing reclaim");
});
it("Show on Home appends a copy when no matching metric exists", async () => {
  await mount(); await click("More for Reclaimable"); await click("Show on Home");
  expect(api.edit).toHaveBeenCalledWith("home", [{ op: "add", panel: header.dashboard.panels[4] }], 0, expect.any(Object), expect.any(String));
  expect(container.querySelector('[data-testid="board"]')?.textContent).toContain("Reclaimable");
});
it.each([
  { value: "codex", title: "CPU for codex", where: [{ column: "agent", op: "eq", value: "codex" }, { column: "agent", op: "ne", value: "other" }] },
  { value: "", title: "CPU for all adapters", where: [{ column: "agent", op: "ne", value: "other" }] },
])("Show on Home makes a variable-dependent widget self-contained for '$value'", async ({ value, title, where }) => {
  const panel: Panel = { ...cpu, title: "CPU for $adapter", source: "costs", where: [{ column: "agent", op: "eq", value: "$adapter" }, { column: "agent", op: "ne", value: "other" }] };
  header.dashboard.panels = [panel];
  header.dashboard.variables = [{ name: "adapter", field: "agent", default: "claude" }];
  header.vars = { adapter: value };
  home.dashboard.variables = [{ name: "project", field: "project" }, { name: "machine", field: "machine" }];
  await mount();
  await click("More for CPU for $adapter"); await click("Show on Home");
  expect(api.edit).toHaveBeenCalledWith("home", [{ op: "add", panel: { ...panel, title, where } }], 0, expect.any(Object), expect.any(String));
  expect(home.dashboard.panels[0]).toEqual({ ...panel, title, where });
  expect(header.dashboard.panels[0]).toEqual(panel);
});

it("disables catalog additions at sixteen widgets", async () => {
  header.dashboard.panels = [...header.dashboard.panels, ...extra.slice(0, 10)];
  await mount(); await click("Add widget");
  const options = [...document.querySelectorAll<HTMLElement>('[role="option"]')];
  expect(options).toHaveLength(2);
  expect(options.every(o => o.getAttribute("aria-disabled") === "true")).toBe(true);
  expect(document.body.textContent).toContain("The header shows at most 16 widgets");
  await act(async () => options[0].click());
  expect(api.edit).not.toHaveBeenCalled();
});

it("Show on Home waits for the destination panel to mount after navigation", async () => {
  history.replaceState(null, "", "/app/?dashboard=other");
  home.dashboard.panels = [{ ...header.dashboard.panels[4], id: "existing", title: "Existing reclaim" }];
  let finish!: (f: Fragment) => void;
  const pending = new Promise<Fragment>(resolve => { finish = resolve; });
  let calls = 0;
  api.open.mockImplementation((id: string) => id === "header" ? Promise.resolve(structuredClone(header)) : id === "home" ? ++calls === 1 ? Promise.resolve(structuredClone(home)) : pending : Promise.resolve({ ...home, id, dashboard: { name: "Other", panels: [] } }));
  const scroll = vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(() => {});
  await mount(); await click("More for Reclaimable"); await click("Show on Home");
  await act(async () => { await new Promise(r => setTimeout(r, 650)); });
  expect(scroll).not.toHaveBeenCalled();
  await act(async () => { finish(structuredClone(home)); });
  expect(scroll).toHaveBeenCalledOnce();
});
