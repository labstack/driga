import { describe, expect, it } from "vitest";
import type { Fragment } from "../panels/types";
import { headerOf } from "./Header";
import { historyOf } from "./History";

function header(stuck: (number | null)[]): Fragment {
  return {
    dashboard: { name: "Header", panels: [] }, observed_at: "2026-03-14T15:00:00Z", local_machine: "local",
    results: ["waiting", "finished", "stuck"].map((id, i) => ({ id, status: "ok", frame: { columns: [{ name: "value", type: "number" }], rows: [[i === 0 ? 3 : 1]] } })),
    history: { attention: stuck.map((n, i) => ({ ts: new Date(Date.parse("2026-03-14T14:01:00Z") + i * 60_000).toISOString(), waiting: i, review: 60 - i, stuck: n })), polls: [] },
  };
}

describe("service history gaps", () => {
  it("keeps all 60 minute positions including interior unknown readings", () => {
    const values = Array.from({ length: 60 }, (_, i) => i === 0 ? 5 : i === 30 ? 2 : i === 59 ? 3 : null);
    const fragment = header(values);
    const kept = historyOf(fragment);
    expect(kept.attention.stuck).toHaveLength(60);
    expect(kept.attention.stuck).toEqual(values);
    expect(kept.attention.waiting).toEqual(Array.from({ length: 60 }, (_, i) => i));
    const attention = headerOf(fragment, null, kept).attention.stuck;
    expect(attention.trend).toEqual(values);
    expect(attention.delta).toBe(-2);
  });
  it.each(["first", "last", "both"])("does not infer an hour's change when the %s endpoint is unknown", endpoint => {
    const values: (number | null)[] = Array.from({ length: 60 }, () => null);
    if (endpoint !== "first" && endpoint !== "both") values[0] = 5;
    if (endpoint !== "last" && endpoint !== "both") values[59] = 3;
    const fragment = header(values);
    const attention = headerOf(fragment, null, historyOf(fragment)).attention.stuck;
    expect(attention.trend).toHaveLength(60);
    expect(attention.trend[0]).toBe(values[0]);
    expect(attention.trend[59]).toBe(values[59]);
    expect(attention.delta).toBe(0);
  });
});
