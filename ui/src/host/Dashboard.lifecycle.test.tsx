// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Fragment, Panel } from "../panels/types";
import { Dashboard } from "./Dashboard";

const api = vi.hoisted(() => ({ confirm: vi.fn() }));
vi.mock("./api", () => ({ confirm: api.confirm }));
vi.mock("../panels/PanelView", () => ({ PanelView: ({ panel }: { panel: Panel }) => <div data-panel={panel.id}>{panel.title}</div> }));
vi.mock("react-grid-layout", async () => {
  const { useRef } = await import("react");
  return { useContainerWidth: () => ({ width: 777, mounted: true, containerRef: useRef<HTMLDivElement>(null) }), GridLayout: ({ children }: { children: ReactNode }) => <div data-grid>{children}</div> };
});
let root: Root;
let container: HTMLDivElement;
let width: number;
let height: number;
let observers: { callback: ResizeObserverCallback; elements: Element[]; disconnected: boolean }[];
const onRemove = vi.fn();
const panels: Panel[] = [{ id: "cards", title: "Sessions", viz: "cards", source: "sessions", width: 12 }, { id: "chart", title: "Memory", viz: "timeseries", source: "activity", width: 12 }];
const fragment = (empty = false): Fragment => ({
  dashboard: { name: "Home", panels },
  results: panels.map(p => ({ id: p.id, status: empty ? "empty" : "ok", frame: { columns: [{ name: "x", type: "string" }], rows: empty ? [] : Array.from({ length: 8 }, () => ["session"]) } })),
  observed_at: "2026-03-14T15:00:00Z", history: { attention: [], polls: [] },
});
async function render(editing = false, empty = false) { await act(async () => root.render(<MantineProvider><Dashboard fragment={fragment(empty)} editing={editing} onLayout={() => {}} onRemove={onRemove} /></MantineProvider>)); }
async function resize() { await act(async () => { for (const observer of observers.filter(o => !o.disconnected)) observer.callback([], {} as ResizeObserver); }); }

beforeEach(() => {
  vi.clearAllMocks();
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  width = 460; height = 960; observers = [];
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => width);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(() => height);
  vi.stubGlobal("ResizeObserver", class {
    observation: typeof observers[number];
    constructor(callback: ResizeObserverCallback) { this.observation = { callback, elements: [], disconnected: false }; observers.push(this.observation); }
    observe(element: Element) { this.observation.elements.push(element); }
    disconnect() { this.observation.disconnected = true; }
    unobserve() {}
  });
  api.confirm.mockResolvedValue(false);
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("Dashboard lifecycle", () => {
  it("waits for both dimensions before mounting panels", async () => {
    width = 0; height = 0;
    await render(); expect(container.querySelector("[data-panel]")).toBeNull();
    height = 960; await resize(); expect(container.querySelector("[data-panel]")).toBeNull();
    width = 460; await resize(); expect(container.querySelectorAll("[data-panel]")).toHaveLength(2);
  });
  it("measures immediately and observes the same connected board after mounting panels", async () => {
    await render();
    expect(container.querySelectorAll("[data-panel]")).toHaveLength(2);
    const active = observers.filter(o => !o.disconnected);
    expect(active).toHaveLength(1);
    expect(active[0].elements).toHaveLength(1);
    expect(active[0].elements[0].isConnected).toBe(true);
    expect(active[0].elements[0].contains(container.querySelector('[data-panel="cards"]'))).toBe(true);
  });
  it("repacks session cards after a width-only resize", async () => {
    await render();
    const row = () => (container.querySelector('[data-panel="cards"]')!.parentElement as HTMLElement).style.gridRow;
    const narrow = row();
    width = 1840; await resize();
    expect(row()).not.toBe(narrow);
    expect(row()).toBe("1 / span 5");
  });
  it("does not mount the editing grid before measurement", async () => {
    width = 0; height = 0;
    await render(true); expect(container.querySelector("[data-grid]")).toBeNull();
    width = 460; height = 960; await resize();
    expect(container.querySelectorAll("[data-panel]")).toHaveLength(2);
  });
  it("keeps a panel when its removal is cancelled", async () => {
    await render(true);
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="Remove Sessions"]')!.click());
    expect(onRemove).not.toHaveBeenCalled();
    expect(api.confirm).toHaveBeenCalledWith("Remove Sessions from this dashboard?");
  });
  it("removes exactly the selected panel after confirmation", async () => {
    api.confirm.mockResolvedValue(true);
    await render(true);
    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="Remove Memory"]')!.click());
    expect(api.confirm).toHaveBeenCalledWith("Remove Memory from this dashboard?");
    expect(onRemove).toHaveBeenCalledExactlyOnceWith("chart");
  });
  it("names the all-clear strip as a group", async () => {
    await render(false, true);
    expect(container.querySelector('[role="group"][aria-label="Nothing to show"]')).not.toBeNull();
  });
});
