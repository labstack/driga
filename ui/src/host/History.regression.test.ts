import { describe, expect, it } from "vitest";
import type { Fragment, Prompt } from "../panels/types";
import { counts, headerOf, nothingKept } from "./Header";
import { historyOf } from "./History";

function header(stuck: (number | null)[]): Fragment {
  return {
    dashboard: { name: "Header", panels: [] }, observed_at: "2026-03-14T15:00:00Z", local_machine: "local",
    results: ["waiting", "finished", "stuck"].map((id, i) => ({ id, status: "ok", frame: { columns: [{ name: "value", type: "number" }], rows: [[i === 0 ? 3 : 1]] } })),
    history: { attention: stuck.map((n, i) => ({ ts: new Date(Date.parse("2026-03-14T14:01:00Z") + i * 60_000).toISOString(), waiting: i, review: 60 - i, stuck: n })), polls: [] },
  };
}
const prompt = (id: string, session_id?: number): Prompt => ({ id, session_id, session: `external-${id}`, cwd: "/repo", tool: "Bash", created_at: "2026-03-14T15:00:00Z" });

describe("service history gaps", () => {
  it("keeps all 60 minute positions including interior unknown readings", () => {
    const values = Array.from({ length: 60 }, (_, i) => i === 0 ? 5 : i === 30 ? 2 : i === 59 ? 3 : null);
    const fragment = header(values);
    const kept = historyOf(fragment);
    expect(kept.attention.stuck).toHaveLength(60);
    expect(kept.attention.stuck).toEqual(values);
    expect(kept.attention.waiting).toEqual(Array.from({ length: 60 }, (_, i) => i));
    const attention = headerOf(fragment, null, [], kept).attention.stuck;
    expect(attention.trend).toEqual(values);
    expect(attention.delta).toBe(-2);
  });
  it.each(["first", "last", "both"])("does not infer an hour's change when the %s endpoint is unknown", endpoint => {
    const values: (number | null)[] = Array.from({ length: 60 }, () => null);
    if (endpoint !== "first" && endpoint !== "both") values[0] = 5;
    if (endpoint !== "last" && endpoint !== "both") values[59] = 3;
    const fragment = header(values);
    const attention = headerOf(fragment, null, [], historyOf(fragment)).attention.stuck;
    expect(attention.trend).toHaveLength(60);
    expect(attention.trend[0]).toBe(values[0]);
    expect(attention.trend[59]).toBe(values[59]);
    expect(attention.delta).toBe(0);
  });
});

describe("header prompt counts", () => {
  it("does not count an existing waiting session twice, and keeps an unbound prompt", () => {
    const fragment = header([]);
    fragment.waiting_sessions = [11];
    const prompts = [prompt("already-waiting", 11), prompt("another-session", 12), prompt("unbound")];
    expect(counts(fragment, prompts)).toEqual({ waiting: 5, review: 1, stuck: 1 });
    expect(headerOf(fragment, null, prompts, nothingKept).attention.waiting.count).toBe(5);
  });
  it("counts unbound prompts when there are no waiting sessions", () => {
    const fragment = header([]);
    fragment.waiting_sessions = [];
    expect(counts(fragment, [prompt("one"), prompt("zero", 0)]).waiting).toBe(5);
  });
  it("counts multiple approvals for a new session once, with unbound prompts separate", () => {
    const fragment = header([]);
    fragment.waiting_sessions = [11, 12];
    expect(counts(fragment, [prompt("waiting-session", 11), prompt("waiting-child", 12), prompt("first", 13), prompt("second", 13), prompt("unbound"), prompt("zero", 0)]).waiting).toBe(6);
  });
});
