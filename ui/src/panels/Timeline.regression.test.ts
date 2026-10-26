import { MantineProvider } from "@mantine/core";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SessionSwimlanesProps } from "../widgets/SessionSwimlanes";
import type { Act } from "./host";
import { lanesOf, PanelView } from "./PanelView";
import type { Cell, Frame, Span } from "./types";

const swimlanes = vi.hoisted(() => ({ render: vi.fn() }));
vi.mock("../widgets/SessionSwimlanes", () => ({ SessionSwimlanes: (props: SessionSwimlanesProps) => { swimlanes.render(props); return null; } }));
const base = Date.parse("2026-03-14T15:00:00Z");
const at = (seconds: number) => new Date(base + seconds * 1000).toISOString();
const frame = (values: Cell[][], spans: Span[] = []): Frame => ({ columns: ["id", "machine_id", "title", "agent", "label", "started_at", "ended_at"].map(name => ({ name, type: "string" })), rows: values, spans });
const row = (machine: string, id = 11): Cell[] => [id, machine, `Session on ${machine}`, "claude", "working", at(0), at(100)];
const wait = (from: number, to: number | null, machine = "a"): Span => ({ session_id: 11, machine_id: machine, kind: "waiting", from: at(from), to: to === null ? null : at(to) });
beforeEach(() => vi.clearAllMocks());

describe("Timeline review regressions", () => {
  it("keeps equal local session IDs and their waits separate across machines", () => {
    const lanes = lanesOf(frame([row("a"), row("b")], [wait(10, 20)]), base + 200_000);
    expect(lanes.map(l => l.id)).toEqual([JSON.stringify(["a", 11]), JSON.stringify(["b", 11])]);
    expect(lanes[0].segments.filter(s => s.state === "waiting")).toEqual([{ from: base + 10_000, to: base + 20_000, state: "waiting" }]);
    expect(lanes[1].segments).toEqual([{ from: base, to: base + 100_000, state: "finished" }]);
  });

  it("clips nested, overlapping, reversed and out-of-range waits to positive segments", () => {
    const spans = [wait(-20, 20), wait(5, 10), wait(15, 40), wait(60, 80), wait(70, null), wait(90, 85), wait(110, 130)];
    const [lane] = lanesOf(frame([row("a")], spans), base + 200_000);
    const segments = lane.segments;
    expect(segments[0].from).toBe(base);
    expect(segments[segments.length - 1].to).toBe(base + 100_000);
    for (const [i, segment] of segments.entries()) {
      expect(segment.from).toBeGreaterThanOrEqual(base);
      expect(segment.to).toBeLessThanOrEqual(base + 100_000);
      expect(segment.to).toBeGreaterThan(segment.from);
      if (i) expect(segment.from).toBe(segments[i - 1].to);
    }
    expect(segments.filter(s => s.state === "finished")).toEqual([{ from: base + 40_000, to: base + 60_000, state: "finished" }]);
  });

  it("opens the selected lane on its own machine when local IDs collide", async () => {
    const f = frame([row("a"), row("b")]);
    const act = vi.fn<Act>(async () => undefined);
    renderToStaticMarkup(createElement(MantineProvider, null, createElement(PanelView, { panel: { id: "flow", title: "Flow", viz: "timeline", source: "flow" }, result: { id: "flow", status: "ok", frame: f }, act })));
    const props = swimlanes.render.mock.calls[swimlanes.render.mock.calls.length - 1][0] as SessionSwimlanesProps;
    props.onSelect!(JSON.stringify(["a", 11]));
    props.onSelect!(JSON.stringify(["b", 11]));
    expect(act.mock.calls.map(([action]) => action.row)).toEqual([
      expect.objectContaining({ session_id: 11, machine_id: "a" }),
      expect.objectContaining({ session_id: 11, machine_id: "b" }),
    ]);
  });
});
