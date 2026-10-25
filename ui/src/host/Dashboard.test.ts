import { describe, expect, it } from "vitest";
import type { Panel, Result } from "../panels/types";
import { layoutOf, packed } from "./Dashboard";

const panel = (id: string, viz: Panel["viz"], width: number, height: Panel["height"] = "m"): Panel => ({ id, title: id, viz, source: "sessions", width, height });
const rows = (id: string, n: number): Result => ({ id, status: n ? "ok" : "empty", frame: { columns: [{ name: "x", type: "string" }], rows: Array.from({ length: n }, () => ["a"]) } });

function covered(items: { x: number; y: number; w: number; h: number }[], height: number): boolean {
  for (let y = 0; y < height; y++) for (let x = 0; x < 12; x++) {
    if (items.filter(i => x >= i.x && x < i.x + i.w && y >= i.y && y < i.y + i.h).length !== 1) return false;
  }
  return true;
}

describe("packed", () => {
  const panels = [panel("inbox", "inbox", 7), panel("orphans", "list", 5), panel("stuck", "table", 12, "s"), panel("sessions", "table", 12), panel("memory", "bar", 6), panel("machine", "timeseries", 6)];
  const byId = new Map(panels.map(p => [p.id, p]));

  it("fills the window with no gaps or overlaps", () => {
    const results = new Map([rows("inbox", 0), rows("orphans", 1), rows("stuck", 1), rows("sessions", 3)].map(r => [r.id, r]));
    const items = packed(layoutOf(panels), byId, results, 1000, 16);
    expect(Math.max(...items.map(i => i.y + i.h))).toBe(16);
    expect(covered(items, 16)).toBe(true);
  });

  it("gives up rows from the tallest panels when content is taller than the window", () => {
    const results = new Map([rows("inbox", 9), rows("orphans", 9), rows("stuck", 9), rows("sessions", 40)].map(r => [r.id, r]));
    const items = packed(layoutOf(panels), byId, results, 1000, 16);
    expect(Math.max(...items.map(i => i.y + i.h))).toBe(16);
    expect(covered(items, 16)).toBe(true);
    expect(items.every(i => i.h >= 2)).toBe(true);
  });
});
