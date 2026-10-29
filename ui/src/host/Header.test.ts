import { describe, expect, it } from "vitest";
import { fleet } from "../fixtures/fleet";
import { fragments } from "../fixtures/fragments";
import { fleetOf } from "../fleet/FleetSpec";
import { tilesOf, headerOf, nothingKept, pollOf, stripsOf } from "./Header";
import { historyOf } from "./History";

const GB = 1024 ** 3;

describe("headerOf", () => {
  const data = fleetOf(fleet, fragments.inbox);
  it("reads panels in spec order and attaches behavior by measure", () => {
    const f = structuredClone(fragments.header);
    f.dashboard.panels = [{ id: "cpu", title: "CPU", viz: "gauge", source: "local_activity", measure: "max:cpu_percent", max: 100 }, { ...f.dashboard.panels[0], id: "custom_wait" }];
    const tiles = tilesOf(f);
    expect(tiles.map(t => t.id)).toEqual(["cpu", "custom_wait"]);
    expect(tiles[0].viz).toBe("gauge");
    expect(tiles[1].behavior.attention).toBe("waiting");
  });
  it("uses the engine-resolved variable title and falls back to the stored title", () => {
    const f = structuredClone(fragments.header);
    f.dashboard.panels = [{ id: "cpu", title: "CPU for $adapter", viz: "gauge", source: "costs", measure: "max:cpu_percent" }];
    f.results = [{ id: "cpu", status: "ok", title: "CPU for codex" }];
    expect(tilesOf(f)[0].title).toBe("CPU for codex");
    f.results[0].title = "";
    expect(tilesOf(f)[0].title).toBe("CPU for $adapter");
    f.results = [];
    expect(tilesOf(f)[0].title).toBe("CPU for $adapter");
  });
  it("keeps missing, empty and errored values unknown", () => {
    const f = structuredClone(fragments.header);
    f.results = [{ id: "waiting", status: "error", message: "bad measure", frame: { columns: [], rows: [[99]] } }, { id: "finished", status: "empty" }];
    const tiles = tilesOf(f);
    expect(tiles.every(t => t.value === null)).toBe(true);
    expect(tiles[0].error).toBe("bad measure");
  });
  it("names the local machine, keeps the fleet and service history", () => {
    const h = headerOf(fragments.header, data, { ...nothingKept, attention: { waiting: [1, null, 3], review: [], stuck: [] } });
    expect(h.tiles.find(t => t.id === "memory")?.total).toBe(32 * GB);
    expect(h.localMachine).toBe("studio");
    expect(h.tiles[0].trend).toEqual([1, null, 3]);
    expect(h.checkedAt).toBe("2026-03-14T15:00:00Z");
    expect(h.machines.map(m => [m.name, m.reachable, m.sessions.waiting])).toEqual([["studio", true, 1], ["build-01", true, 0], ["laptop-02", false, 0]]);
    expect(headerOf(fragments.header, null, nothingKept).localMachine).toBe("studio-id");
  });
  it("turns kept polls into strips, once there is more than one", () => {
    const h = headerOf(fragments.header, data, nothingKept);
    expect(h.machines.map(pollOf)).toEqual(["waiting", "stuck", "offline"]);
    expect(stripsOf({ ...nothingKept, polls: { "studio-id": ["working", "waiting"], "build-id": ["stuck"] } }, h.machines)).toMatchObject({ "studio-id": { cells: [{ state: "working" }, { state: "waiting" }], from: "2 polls" } });
    expect(stripsOf({ ...nothingKept, polls: { "build-id": ["stuck"] } }, h.machines)).toEqual({});
  });
});

describe("historyOf", () => {
 it("reads the same service history after a reload, with no browser accumulation", () => {
  const first = historyOf(fragments.header);
  expect(first.attention.waiting).toHaveLength(60);
  expect(first.attention.waiting[0]).toBe(1);
  expect(first.attention.waiting[59]).toBe(3);
  expect(first.polls["studio-id"]).toHaveLength(30);
  expect(historyOf(structuredClone(fragments.header))).toEqual(first);
 });
 it("keeps unknown stuck readings absent and never substitutes current counts", () => {
  const f = structuredClone(fragments.header);
  f.history = { attention: [{ts: f.observed_at, waiting: 1, review: 2, stuck: null}], polls: [] };
  expect(historyOf(f).attention).toEqual({ waiting: [1], review: [2], stuck: [null] });
 });
});
