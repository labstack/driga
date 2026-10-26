import { describe, expect, it } from "vitest";
import { fleet } from "../fixtures/fleet";
import { fragments } from "../fixtures/fragments";
import { fleetOf } from "../fleet/FleetSpec";
import { changeWords, counts, headerOf, nothingKept, pollOf, stripsOf } from "./Header";
import { historyOf } from "./History";

const GB = 1024 ** 3;

describe("headerOf", () => {
  const data = fleetOf(fleet, fragments.inbox);
  it("reads the server's reconciled counts", () => {
    expect(counts(fragments.header)).toEqual({ waiting: 3, review: 3, stuck: 1 });
    const h = headerOf(fragments.header, data, nothingKept);
    expect(h.attention.waiting).toEqual({ count: 3, trend: [], delta: 0 });
    expect(h.checkedAt).toBe("2026-03-14T15:00:00Z");
  });
  it("names the local machine from the fleet and dots every machine", () => {
    const h = headerOf(fragments.header, data, nothingKept);
    expect(h.memory.machine).toBe("studio");
    expect(h.memory.total).toBe(32 * GB);
    expect(h.machines.map(m => [m.name, m.reachable, m.sessions.waiting])).toEqual([["studio", true, 1], ["build-01", true, 0], ["laptop-02", false, 0]]);
    expect(h.hygiene.leftover.count).toBe(2);
  });
  it("manages without the fleet: the local machine by its id, no dots, no leftover count", () => {
    const h = headerOf(fragments.header, null, nothingKept);
    expect(h.machines).toEqual([]);
    expect(h.memory.machine).toBe("studio-id");
    expect(h.hygiene.leftover.count).toBe(2);
  });
  it("takes the hour's trend and delta from what the app kept", () => {
    const h = headerOf(fragments.header, data, { ...nothingKept, attention: { waiting: [1, 1, 2, 3], review: [3], stuck: [2, 1] } });
    expect(h.attention.waiting.delta).toBe(2);
    expect(changeWords(h.attention.waiting)).toBe("up 2 in the last hour");
    expect(changeWords(h.attention.stuck)).toBe("down 1 in the last hour");
    expect(changeWords(h.attention.review)).toBe("not enough history for an hour’s change");
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

it("uses the service leftover count beyond the Fleet table limit",()=>{
 const header={...fragments.header,leftover_count:201};
 expect(headerOf(header,fleetOf(fleet,fragments.inbox),nothingKept).hygiene.leftover.count).toBe(201);
 expect(headerOf(header,null,nothingKept).hygiene.leftover.count).toBe(201);
});
