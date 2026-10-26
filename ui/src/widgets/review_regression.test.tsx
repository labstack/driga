// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chartTheme, type Option, type Point, type Theme } from "../panels/charts";
import { ChartCard } from "./ChartCard";
import { heatmap, timeSeries } from "./charts";
import { MachineCard, type Machine } from "./MachineCard";
import { Sparkline } from "./Measure";
import { SessionCard, type Session } from "./SessionCard";
import { stateOf, StatusBadge } from "./StatusBadge";
import { TrackerBar, type TrackerCell } from "./TrackerBar";
import { agentOf } from "./types";

const charts = vi.hoisted(() => ({ options: [] as unknown[], labels: [] as string[] }));
vi.mock("../panels/Chart", () => ({ Chart: ({ option, label }: { option: unknown; label: string }) => { charts.options.push(option); charts.labels.push(label); return <div role="img" aria-label={label} />; } }));
let root: Root;
let container: HTMLDivElement;
const machine: Machine = { name: "studio", reachable: true, lastSeen: "2026-03-14T15:00:00Z", memUsed: 80, memTotal: 100, diskTotal: 500, diskFree: 120, sessions: { working: 2, waiting: 1, stuck: 0, idle: 3 }, leftover: { trees: 0, bytes: 0 } };
const session: Session = { id: "11", title: "Fix uploads", state: "working", agent: "claude", project: "repo", branch: "main", machine: "studio", lastAt: "2026-03-14T15:00:00Z", turns: 3, files: 2, uncommitted: 0, memoryBytes: 100, startedAt: "2026-03-14T14:00:00Z", trend: [1, 2, 3] };
async function render(children: ReactNode) { await act(async () => { root.render(<MantineProvider>{children}</MantineProvider>); }); }

beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  charts.options = []; charts.labels = [];
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.restoreAllMocks(); });

describe("widget review regressions", () => {
  it("does not draw a full disk when its free-space sample is unknown", async () => {
    await render(<MachineCard machine={{ ...machine, diskFree: undefined }} />);
    expect(container.querySelector('[role="img"][aria-label^="Disk:"]')).toBeNull();
    await render(<MachineCard machine={{ ...machine, diskFree: 0 }} />);
    expect(container.querySelector('[role="img"][aria-label^="Disk:"]')?.getAttribute("aria-label")).toContain("100%");
  });
  it("names machine pressure and session counts on accessible groups", async () => {
    await render(<MachineCard machine={machine} />);
    expect(container.querySelector('[role="group"][aria-label="Pressure"]')).not.toBeNull();
    expect(container.querySelector('[role="group"][aria-label="2 working, 1 waiting, 0 stuck, 3 idle"]')).not.toBeNull();
  });
  it("exposes the status badge as one named image", async () => {
    await render(<StatusBadge state="waiting" inferred />);
    expect(container.querySelector('[role="img"][aria-label="waiting, inferred"]')).not.toBeNull();
  });
  it.each(["__proto__", "constructor", "toString"])("rejects inherited agent and state key %s", key => {
    expect(agentOf(key)).toBeUndefined();
    expect(stateOf(key)).toEqual({ state: undefined, label: key, inferred: false });
    expect(stateOf(`~${key}`)).toEqual({ state: undefined, label: key, inferred: true });
    expect(agentOf("claude")).toBe("claude");
  });
  it("handles hover over a zero-width tracker without inventing a cell", async () => {
    const cells: TrackerCell[] = [{ state: "working", label: "first" }];
    await render(<TrackerBar cells={cells} label="Polls" />);
    const bar = container.querySelector<HTMLElement>('[role="img"]')!;
    vi.spyOn(bar, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 0, 12));
    await act(async () => bar.dispatchEvent(new MouseEvent("mousemove", { clientX: 10, bubbles: true })));
    expect(container.querySelectorAll(".w-cell")).toHaveLength(1);
    expect(container.querySelector('.w-cell[style]')).toBeNull();
  });
  it("keeps a tracker usable when the hovered cell disappears after refresh", async () => {
    const cells: TrackerCell[] = [{ state: "working", label: "first" }, { state: "waiting", label: "second" }];
    await render(<TrackerBar cells={cells} label="Polls" />);
    const bar = container.querySelector<HTMLElement>('[role="img"]')!;
    vi.spyOn(bar, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 100, 12));
    await act(async () => bar.dispatchEvent(new MouseEvent("mousemove", { clientX: 75, bubbles: true })));
    expect(container.querySelectorAll('.w-cell[style]')).toHaveLength(1);
    await render(<TrackerBar cells={cells.slice(0, 1)} label="Polls" />);
    expect(container.querySelectorAll(".w-cell")).toHaveLength(1);
    expect(container.querySelector('[role="img"]')?.getAttribute("aria-label")).toBe("Polls: 1 working");
  });
  it("uses a sparkline's supplied window and the session's 30-minute window", async () => {
    await render(<Sparkline title="Memory" trend={[1, 2]} window="15 minutes" />);
    expect(charts.labels.at(-1)).toBe("Memory over the last 15 minutes");
    const custom = (charts.options.at(-1) as (t: Theme) => Option)(chartTheme("dark"));
    expect((custom.aria as { label: { description: string } }).label.description).toBe("Memory over the last 15 minutes");
    await render(<SessionCard session={session} />);
    expect(charts.labels.at(-1)).toBe("Fix uploads: tool calls per minute over the last 30 minutes");
    const sessionOption = (charts.options.at(-1) as (t: Theme) => Option)(chartTheme("dark"));
    expect((sessionOption.aria as { label: { description: string } }).label.description).toBe("Fix uploads: tool calls per minute over the last 30 minutes");
  });
  it("keeps ChartCard's option stable when series is omitted", async () => {
    await render(<ChartCard title="Memory" />); const first = charts.options.at(-1);
    await render(<ChartCard title="Memory" />);
    expect(charts.options.at(-1)).toBe(first);
  });
  it("keeps an empty ChartCard's option stable across rerenders", async () => {
    await render(<ChartCard title="Memory" state="empty" />); const first = charts.options.at(-1);
    await render(<ChartCard title="Memory" state="empty" />);
    expect(charts.options.at(-1)).toBe(first);
  });
  it("builds a peak for a time series larger than the engine's argument limit", () => {
    const points = Array.from({ length: 250_000 }, (_, i): Point => [i, i]);
    const option = timeSeries({ series: [{ name: "Calls", points }], from: 0, to: points.length, format: String, label: "Calls" }, chartTheme("dark"));
    const series = option.series as { data: Point[]; markLine: { label: { formatter: string } } }[];
    expect(series[0].data).toHaveLength(250_000);
    expect(series[0].markLine.label.formatter).toBe("peak 249999");
  });
  it("builds a heatmap scale for more cells than the engine's argument limit", () => {
    const values = [Array.from({ length: 250_000 }, (_, i) => i)];
    const option = heatmap({ rows: ["repo"], cols: [], values, format: String, label: "Calls", unit: "calls" }, chartTheme("dark"));
    expect((option.visualMap as { max: number }).max).toBe(249_999);
    expect((option.series as { data: number[][] }[])[0].data).toHaveLength(250_000);
  });
});
