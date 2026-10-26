import { describe, expect, it } from "vitest";
import { summary } from "./charts";
import type { Frame, Panel } from "./types";

const panel = (viz: Panel["viz"]): Panel => ({ id: "p", title: "p", viz, source: "activity" });
const GB = 1024 ** 3;

describe("summary", () => {
  it("names the largest bar and the total", () => {
    const frame: Frame = { columns: [{ name: "project", type: "string" }, { name: "mem", type: "number", unit: "bytes" }], rows: [["api", 1 * GB], ["web", 3 * GB]] };
    expect(summary(frame, panel("bar"))).toBe("web most, 3 GB of 4 GB");
  });
  it("gives one series' latest reading and its peak", () => {
    const frame: Frame = { columns: [{ name: "ts", type: "time" }, { name: "series", type: "string" }, { name: "value", type: "number", unit: "bytes" }], rows: [["2026-10-25T10:00:00Z", "", 30 * GB], ["2026-10-25T10:01:00Z", "", 25 * GB]] };
    expect(summary(frame, panel("timeseries"))).toBe("25 GB now, peak 30 GB");
  });
  it("names the highest of several series now", () => {
    const frame: Frame = { columns: [{ name: "ts", type: "time" }, { name: "series", type: "string" }, { name: "value", type: "number", unit: "bytes" }], rows: [["2026-10-25T10:00:00Z", "volt", 30 * GB], ["2026-10-25T10:00:00Z", "spark", 10 * GB], ["2026-10-25T10:01:00Z", "volt", 5 * GB], ["2026-10-25T10:01:00Z", "spark", 12 * GB]] };
    expect(summary(frame, panel("timeseries"))).toBe("spark highest now at 12 GB");
  });
  it("says nothing without data", () => {
    expect(summary({ columns: [], rows: [] }, panel("bar"))).toBe("");
  });
});
