// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import type { Chart } from "../panels/Chart";
import { chartTheme } from "../panels/charts";
import type { Cell, Fragment, Frame } from "../panels/types";
import { FleetInspector } from "./FleetInspector";
import { FleetRelationships } from "./FleetModel";
import { FleetPage } from "./FleetPage";
import { FleetSankey } from "./FleetSankey";
import { fleetSankey, selectionOf } from "./FleetSankeyOption";
import type { FleetData, FleetMachine, FleetSession } from "./FleetTypes";

const charts = vi.hoisted(() => ({ render: vi.fn() }));
vi.mock("../panels/Chart", () => ({ Chart: (props: ComponentProps<typeof Chart>) => { charts.render(props); return <div data-chart={props.label} />; } }));

const at = "2026-03-14T15:00:00Z";
const machine = (id = "local"): FleetMachine => ({ id, name: id, local: id === "local", reachable: true, lastSeen: at, memoryUsed: 100, memoryTotal: 1000 });
const session = (id: string, extra: Partial<FleetSession> = {}): FleetSession => ({ id, title: `Session ${id}`, project: "alpha", machine: "local", state: "working", agent: "claude", memory: 100, lastActivity: at, ...extra });
const data = (sessions: FleetSession[], machines = [machine()]): FleetData => ({ machines, projects: [{ id: "alpha", name: "Alpha", repo: "/alpha" }], sessions });
const frame = (columns: string[], rows: Cell[][]): Frame => ({ columns: columns.map(name => ({ name, type: "string" })), rows });
const fragment = (ids: string[] = ["2"]): Fragment => ({
  history: { attention: [], polls: [] }, dashboard: { name: "Fleet", panels: [] }, observed_at: at,
  results: [
    { id: "machines", status: "ok", frame: frame(["machine_id", "machine", "state", "mem_total_bytes"], [["local", "local", "local", 1000]]) },
    { id: "sessions", status: "ok", frame: frame(["id", "title", "machine_id", "project", "label"], ids.map(id => [id, `Session ${id}`, "local", "alpha", "working"])) },
    { id: "heatmap", status: "ok", frame: frame(["ts", "series", "value"], [["2026-03-14T14:00:00Z", "beta", 4], ["2026-03-14T13:00:00Z", "alpha", 2], ["2026-03-14T14:00:00Z", "alpha", 3]]) },
  ],
});

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  vi.clearAllMocks();
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });
async function render(node: ReactNode) { await act(async () => root.render(<MantineProvider>{node}</MantineProvider>)); }
const noop = () => undefined;
function chart(label: string): ComponentProps<typeof Chart> {
  const props = charts.render.mock.calls.map(([props]) => props as ComponentProps<typeof Chart>).filter(p => p.label === label);
  expect(props.length).toBeGreaterThan(0);
  return props[props.length - 1];
}

describe("Fleet review regressions", () => {
  it("keeps unknown empty turn and file arrays quiet", async () => {
    await render(<FleetInspector data={data([session("1", { turns: [], files: [] })])} selection={{ kind: "session", id: "1" }} onSelect={noop} onOpen={noop} onReply={noop} />);
    expect(container.textContent).not.toContain("0 turns");
    expect(container.textContent).not.toContain("0 files");
    await render(<FleetInspector data={data([session("1", { turns: ["ok"], files: ["main.go"] })])} selection={{ kind: "session", id: "1" }} onSelect={noop} onOpen={noop} onReply={noop} />);
    expect(container.textContent).toContain("1 turn");
    expect(container.textContent).toContain("1 file");
  });

  it("uses readable text on the overlap badge's danger tint", async () => {
    await render(<FleetInspector data={data([session("1", { files: ["main.go"] }), session("2", { files: ["main.go"] })])} selection={{ kind: "session", id: "1" }} onSelect={noop} onOpen={noop} onReply={noop} />);
    const badge = container.querySelector<HTMLElement>('[title="Also edited by Session 2"]');
    expect(badge?.textContent).toBe("also #2");
    expect(badge?.style.color).toBe("var(--driga-text)");
  });

  it("omits unknown disk pressure while preserving a measured full disk", async () => {
    const m = { ...machine(), diskTotal: 1000, diskFree: undefined };
    await render(<FleetInspector data={data([], [m])} selection={{ kind: "machine", id: "local" }} onSelect={noop} onOpen={noop} onReply={noop} />);
    expect(container.querySelector('[aria-label^="Disk on local"]')).toBeNull();
    await render(<FleetInspector data={data([], [{ ...m, diskFree: 0 }])} selection={{ kind: "machine", id: "local" }} onSelect={noop} onOpen={noop} onReply={noop} />);
    expect(container.querySelector('[aria-label^="Disk on local"]')?.getAttribute("aria-label")).toContain("100%");
  });

  it("clears a vanished machine selection and shows the remaining fleet", async () => {
    const onSelect = vi.fn();
    const a = machine("a"), b = machine("b");
    const first = data([session("1", { machine: "a" }), session("2", { machine: "b" })], [a, b]);
    await render(<FleetSankey {...first} defaultSelected={{ kind: "machine", id: "a" }} defaultMode="tree" onSelect={onSelect} onOpen={noop} onReply={noop} />);
    expect(container.querySelector('[title="Session 1"]')).not.toBeNull();
    await render(<FleetSankey {...data([session("2", { machine: "b" })], [b])} defaultSelected={{ kind: "machine", id: "a" }} defaultMode="tree" onSelect={onSelect} onOpen={noop} onReply={noop} />);
    expect(onSelect).toHaveBeenCalledWith(null);
    expect(container.querySelector('[title="Session 2"]')).not.toBeNull();
  });

  it("sends jump actions only for positive safe integer session IDs", async () => {
    const act = vi.fn(async () => undefined);
    await render(<FleetPage fragment={fragment(["unknown", "0", "-1", "1.5", "9007199254740993", "2"])} act={act} />);
    await actReactClick(container.querySelector<HTMLInputElement>('input[value="tree"]')!);
    for (const button of container.querySelectorAll<HTMLButtonElement>('button[aria-label^="Open Session"]')) await actReactClick(button);
    expect(act).toHaveBeenCalledExactlyOnceWith({ kind: "jump", row: { session_id: 2, machine_id: "local" } });
  });

  it("keeps the heatmap option stable when the service fragment is unchanged", async () => {
    const f = fragment();
    const label = "Tool calls by project, last 24 hours";
    await render(<FleetPage fragment={f} />);
    const option = chart(label).option;
    await render(<FleetPage fragment={f} />);
    expect(chart(label).option).toBe(option);
    const series = option(chartTheme("dark")).series as { data: number[][] }[];
    expect(series[0].data).toEqual([[0, 0, 0], [1, 0, 4], [0, 1, 2], [1, 1, 3]]);
  });

  it("indexes duplicate edited paths once and isolates machine and project", () => {
    const a = session("1", { files: ["a.go", "a.go", "b.go"] });
    const b = session("2", { files: ["a.go", "b.go"] });
    const otherMachine = session("3", { machine: "remote", files: ["a.go"] });
    const otherProject = session("4", { project: "beta", files: ["a.go"] });
    const sessions = [a, b, otherMachine, otherProject];
    const index = new FleetRelationships(data(sessions));
    expect(index.overlaps(a)).toEqual([{ with: b, files: ["a.go", "b.go"] }]);
    expect(index.overlaps(b)).toEqual([{ with: a, files: ["a.go", "b.go"] }]);
    expect(index.overlaps(otherMachine)).toEqual([]);
    expect(index.overlaps(otherProject)).toEqual([]);
  });

  it("builds Sankey overlap evidence once for the fleet", () => {
    let fileReads = 0;
    const sessions = Array.from({ length: 100 }, (_, i) => {
      const s = session(String(i));
      Object.defineProperty(s, "files", { get: () => { fileReads++; return [`file-${i}.go`]; } });
      return s;
    });
    fleetSankey(data(sessions), chartTheme("dark"));
    expect(fileReads).toBeLessThanOrEqual(sessions.length * 2);
  });
});

async function actReactClick(element: HTMLElement) {
  expect(element).not.toBeNull();
  await act(async () => element.click());
}

it("keeps Fleet session selections distinct between machines", async () => {
 const local=session("11",{title:"Local session",files:["local.go"]});
 const remote=session("11",{machine:"remote",title:"Remote session",files:["remote.go"]});
 const fleet=data([local,remote],[machine(),machine("remote")]);
 const option=fleetSankey(fleet,chartTheme("dark"));
 const series=(option.series as {data:{name:string}[]}[])[0];
 const names=series.data.filter(n=>n.name.startsWith("s:")).map(n=>n.name);
 expect(new Set(names).size).toBe(2);
 const selected=selectionOf({name:`s:${JSON.stringify(["remote","11"])}`});
 expect(selected).toEqual({kind:"session",id:"11",machine:"remote"});
 await render(<FleetInspector data={fleet} selection={selected} onSelect={noop} onOpen={noop} onReply={noop} />);
 expect(container.textContent).toContain("Remote session");expect(container.textContent).toContain("remote.go");expect(container.textContent).not.toContain("local.go");
 await render(<FleetInspector data={fleet} selection={{kind:"session",id:"11"}} onSelect={noop} onOpen={noop} onReply={noop}/>);
 expect(container.textContent).toContain("Nothing selected");
});
