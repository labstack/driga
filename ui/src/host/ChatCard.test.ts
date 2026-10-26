import { describe, expect, it } from "vitest";
import type { Panel, Result } from "../panels/types";
import { changes, problems, reading } from "./ChatCard";

const panel = (viz: Panel["viz"]): Panel => ({ id: "p", title: "P", viz, source: "sessions" });
const GB = 1024 ** 3;

describe("reading", () => {
  it("says a stat's value and its total", () => {
    const r: Result = { id: "p", status: "ok", total: 36 * GB, frame: { columns: [{ name: "v", type: "number", unit: "bytes" }], rows: [[25 * GB]] } };
    expect(reading(panel("stat"), r)).toBe("25 GB of 36 GB");
  });
  it("counts a table's rows and keeps an empty panel's own message", () => {
    expect(reading(panel("table"), { id: "p", status: "ok", frame: { columns: [{ name: "x", type: "string" }], rows: [["a"], ["b"]] } })).toBe("2 rows");
    expect(reading(panel("inbox"), { id: "p", status: "empty", message: "No turns finished today." })).toBe("No turns finished today.");
  });
  it("says what a text panel holds", () => {
    const table = { ...panel("text"), content: "| Project | Total |\n|---|---|\n| monk | 30 GB |\n| driga | 5 GB |\n\nDisk free: 111 GB." };
    expect(reading(table)).toBe("table, 2 rows");
    expect(reading({ ...panel("text"), content: "Measured with du." })).toBe("note");
  });
  it("says why a panel failed", () => {
    expect(reading(panel("bar"), { id: "p", status: "error", message: "unknown column" })).toBe("unknown column");
  });
});

describe("changes", () => {
  it("names each edit by its panel", () => {
    const before = { name: "Home", panels: [{ id: "mem", title: "Memory", viz: "bar", source: "costs" } as Panel] };
    expect(changes({ tool: "edit_dashboard", input: { id: "home", operations: [{ op: "add", panel: { id: "q", title: "Quota", viz: "stat", source: "summary" } }, { op: "remove", id: "mem" }] } }, before))
      .toEqual(["Added Quota", "Removed Memory"]);
  });
  it("says what a create or restore did", () => {
    // The card's name and its panels already say what was saved.
    expect(changes({ tool: "create_dashboard", input: { dashboard: { name: "monk", panels: [panel("stat"), panel("table")] } } })).toEqual(["Created"]);
    expect(changes({ tool: "replace_dashboard", input: { id: "monk", dashboard: { name: "monk", panels: [panel("stat")] } } })).toEqual(["Replaced"]);
    expect(changes({ tool: "restore_dashboard_version", input: { id: "home", version: 3 } })).toEqual(["Restored version 3"]);
  });
});

describe("problems", () => {
  it("counts the panels that failed and says nothing when none did", () => {
    const ok: Result = { id: "a", status: "ok" };
    const bad: Result = { id: "b", status: "error", message: "unknown column" };
    expect(problems([ok, ok])).toBe("");
    expect(problems([ok, bad])).toBe("1 failed");
    expect(problems([bad, bad])).toBe("2 failed");
  });
});
