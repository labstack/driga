// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { within } from "@testing-library/dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { fragments } from "../fixtures/fragments";
import type { Fragment, Panel } from "../panels/types";
import { PanelView } from "../panels/PanelView";
import { ClustersHeader } from "./ClustersHeader";
import { headerOf, nothingKept } from "./Header";
import { freshFragment } from "./Header-fixtures";

const size = vi.hoisted(() => ({ width: 4000, intrinsic: 144 }));
vi.mock("@mantine/hooks", async importOriginal => ({ ...await importOriginal<typeof import("@mantine/hooks")>(), useElementSize: (options?: unknown) => ({ ref: () => {}, width: options ? size.intrinsic : size.width, height: 56 }) }));
vi.mock("../panels/Chart", () => ({ Chart: ({ label }: { label: string }) => <div role="img" aria-label={label} /> }));
let root: Root;
let container: HTMLDivElement;
const onFilter = vi.fn();
const cpu: Panel = { id: "cpu", title: "CPU", viz: "gauge", source: "local_activity", measure: "max:cpu_percent", max: 100 };
function edited(): Fragment {
  const f = structuredClone(fragments.header);
  f.dashboard.panels = [cpu, ...f.dashboard.panels.filter(p => p.id !== "reclaim")];
  f.results.push({ id: "cpu", status: "ok", frame: { columns: [{ name: "value", type: "number", unit: "percent" }], rows: [[42]] } });
  return f;
}
async function render(f: Fragment) {
  await act(async () => root.render(<MantineProvider><ClustersHeader data={headerOf(f, null, nothingKept)} chatOpen={false} onFilter={onFilter} onShow={vi.fn()} onAsk={vi.fn()} onOpenFleet={vi.fn()} onSearch={vi.fn()} onToggleChat={vi.fn()} /></MantineProvider>));
}
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  size.width = 4000; size.intrinsic = 144;
  onFilter.mockClear();
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });
it("exposes exactly one Need you button and keeps measuring copies passive", async () => {
  await render(fragments.header);
  const buttons = within(container).getAllByRole("button", { name: /^Need you/ });
  expect(buttons).toHaveLength(1);
  const measuring = container.querySelector('[inert][aria-hidden="true"]')!;
  expect(measuring).not.toBeNull();
  expect(measuring.querySelector('button, a[href], input, select, textarea, [tabindex], [contenteditable="true"]')).toBeNull();
  await act(async () => buttons[0].click());
  expect(onFilter).toHaveBeenCalledWith("waiting");
});
it("first load shows leftover bytes followed by the tree count caption", async () => {
  await render(freshFragment);
  const leftover = within(container).getByRole("button", { name: /^Leftover/ });
  expect(leftover.textContent).toMatch(/Leftover\s*1\.8\s*GB\s*· 2 trees/);
});
it.each([[0, "0 trees"], [1, "1 tree"], [2, "2 trees"]])("names leftover count captions for %i trees even when the panel is renamed", async (count, caption) => {
  const f = structuredClone(fragments.header);
  const panel = { ...f.dashboard.panels.find(p => p.id === "leftover")!, id: "renamed", title: "Process memory", caption: "max:leftover" };
  f.dashboard.panels = [panel];
  f.results = [{ ...f.results.find(r => r.id === "leftover")!, id: panel.id, caption: count }];
  await render(f);
  expect(within(container).getByRole("button", { name: /^Process memory/ }).textContent).toContain(`· ${caption}`);
});
it("does not call an unrelated caption measure trees", async () => {
  const f = structuredClone(freshFragment);
  f.dashboard.panels = f.dashboard.panels.filter(p => p.id === "leftover").map(p => ({ ...p, caption: "sum:events" }));
  await render(f);
  const text = within(container).getByRole("button", { name: /^Leftover/ }).textContent;
  expect(text).toContain("· 2");
  expect(text).not.toContain("trees");
});
it("edited spec shows in header", async () => {
  await render(edited());
  expect(container.textContent).toContain("CPU");
  expect(container.textContent).not.toContain("Reclaimable");
  expect([...container.querySelectorAll(".w-tile")].find(t => t.textContent?.includes("CPU"))?.textContent).toContain("studio-id");
});
it("errored panel shows dash and its message in the tooltip", async () => {
  const f = edited(); f.results = [{ id: "cpu", status: "error", message: "CPU unavailable" }];
  await render(f);
  const tile = [...container.querySelectorAll<HTMLElement>(".w-tile")].find(el => el.textContent?.includes("CPU"))!;
  expect(tile.textContent).toContain("—");
  await act(async () => { tile.focus(); await new Promise(r => setTimeout(r, 350)); });
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain("CPU unavailable");
});
it("attention tile filters wherever placed", async () => {
  const f = edited(); const stuck = f.dashboard.panels.find(p => p.id === "stuck")!;
  f.dashboard.panels = [stuck, ...f.dashboard.panels.filter(p => p !== stuck)];
  await render(f);
  await act(async () => { container.querySelector<HTMLButtonElement>(".w-tile")!.click(); });
  expect(onFilter).toHaveBeenCalledWith("stuck");
});
it("overflow button lists hidden tiles with live values and attention actions", async () => {
  size.width = 640;
  const f = edited();
  f.dashboard.panels = Array.from({ length: 18 }, (_, i) => ({ ...cpu, id: `cpu${i}`, title: `CPU ${i}` }));
  f.dashboard.panels[17] = fragments.header.dashboard.panels.find(p => p.id === "stuck")!;
  f.results = f.dashboard.panels.map(p => ({ id: p.id, status: "ok", frame: { columns: [{ name: "value", type: "number" }], rows: [[17]] } }));
  await render(f);
  const more = [...container.querySelectorAll("button")].find(b => /^\+\d+$/.test(b.textContent ?? ""))!;
  expect(more).toBeTruthy();
  expect(document.querySelector('[data-header-tile="stuck"]')).toBeNull();
  await act(async () => more.click());
  const menu = document.querySelector('[role="menu"]')!;
  expect(menu.querySelectorAll('[role="menuitem"]').length).toBeGreaterThan(0);
  expect(menu.textContent).toContain("CPU 16");
  expect(menu.textContent).toContain("17");
  await act(async () => { [...menu.querySelectorAll<HTMLButtonElement>(".w-tile")].find(b => b.textContent?.includes("Stuck"))!.click(); });
  expect(onFilter).toHaveBeenCalledWith("stuck");
  await act(async () => { await new Promise(r => setTimeout(r, 350)); });
  expect(document.querySelector('[data-header-tile="stuck"]')).toBeNull();
  expect(document.querySelector('[role="menu"]')).toBeNull();
});
it("stat caption renders in the header and on Home, including zero", async () => {
  const f = structuredClone(fragments.header);
  const panel = f.dashboard.panels.find(p => p.id === "leftover")!;
  const result = { ...f.results.find(r => r.id === panel.id)!, caption: 0 };
  f.dashboard.panels = [panel]; f.results = [result];
  await render(f);
  expect(container.querySelector(".w-tile")?.textContent).toContain("· 0");
  await act(async () => root.render(<MantineProvider><PanelView panel={panel} result={result} /></MantineProvider>));
  expect(container.textContent).toContain("· 0");
});

it("stat caption renders on Home", async () => {
  const panel = fragments.header.dashboard.panels.find(p => p.id === "leftover")!;
  const result = { ...fragments.header.results.find(r => r.id === panel.id)!, caption: 7 };
  await act(async () => root.render(<MantineProvider><PanelView panel={panel} result={result} /></MantineProvider>));
  expect(container.textContent).toContain("· 7");
});

it("renders the bar for a known zero total", async () => {
  const f = structuredClone(fragments.header);
  f.dashboard.panels = [{ id: "up", title: "Machines up", viz: "stat", source: "summary", measure: "sum:machines_reachable", total: "sum:machines" }];
  f.results = [{ id: "up", status: "ok", total: 0, frame: { columns: [{ name: "value", type: "number" }], rows: [[0]] } }];
  await render(f);
  expect(container.querySelector('[role="progressbar"]')).not.toBeNull();
});

it("keeps Memory's total bar without a competing trend or delta in the header", async () => {
  await render(fragments.header);
  const memory = within(container).getByRole("button", { name: /^Memory/ });
  expect(within(memory).getByRole("progressbar")).toBeTruthy();
  expect(memory.querySelector('[role="img"]')).toBeNull();
  expect(memory.textContent).not.toContain("1h");
});

it("budgets measured tile and overflow widths, and stays stable on rerender", async () => {
  size.width = 800; size.intrinsic = 330;
  const f = edited();
  await render(f);
  const visibleIds = () => [...container.querySelectorAll("[data-header-tile]")].map(el => el.getAttribute("data-header-tile"));
  expect(visibleIds()).toEqual(["cpu"]);
  expect(container.querySelector('button[aria-label="+5"]')).toBeTruthy();
  await render(f);
  expect(visibleIds()).toEqual(["cpu"]);
  size.width = 1100; await render(f);
  expect(visibleIds()).toEqual(["cpu", "waiting"]);
});

it("keeps all six default tiles in the row when their measured widths and gaps fit exactly", async () => {
  size.width = 6 * size.intrinsic + 5 * 6;
  await render(fragments.header);
  expect([...container.querySelectorAll("[data-header-tile]")].map(el => el.getAttribute("data-header-tile")))
    .toEqual(fragments.header.dashboard.panels.map(p => p.id));
  expect(within(container).queryByRole("button", { name: /^\+\d+$/ })).toBeNull();
  size.width -= 0.5;
  await render(fragments.header);
  expect(within(container).getByRole("button", { name: "+2" })).toBeTruthy();
});

it("shows a gauge's value once, without an out-of total", async () => {
  const f = edited();
  f.results = f.results.map(r => r.id === "cpu" ? { ...r, total: 100 } : r);
  await render(f);
  const tile = container.querySelector('[data-header-tile="cpu"]')!;
  expect(tile.textContent).not.toMatch(/\/\s*100/);
  expect(tile.textContent).toBe("42CPU · studio-id");
});
