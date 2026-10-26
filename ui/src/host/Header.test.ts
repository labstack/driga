import { describe, expect, it } from "vitest";
import { fleet } from "../fixtures/fleet";
import { fragments, storyNow } from "../fixtures/fragments";
import { fleetOf } from "../fleet/FleetSpec";
import { changeWords, counts, headerOf, nothingKept, pollOf, stripsOf } from "./Header";
import { trendOf } from "./History";

const GB = 1024 ** 3;

describe("headerOf", () => {
  const data = fleetOf(fleet, fragments.inbox);
  it("reads the counts, adding Chat's prompts to what needs the person", () => {
    expect(counts(fragments.header, 2)).toEqual({ waiting: 5, review: 3, stuck: 1 });
    const h = headerOf(fragments.header, data, 0, nothingKept);
    expect(h.attention.waiting).toEqual({ count: 3, trend: [3], delta: 0 });
    expect(h.checkedAt).toBe("2026-03-14T15:00:00Z");
  });
  it("names the local machine from the fleet and dots every machine", () => {
    const h = headerOf(fragments.header, data, 0, nothingKept);
    expect(h.memory.machine).toBe("studio");
    expect(h.memory.total).toBe(32 * GB);
    expect(h.machines.map(m => [m.name, m.reachable, m.sessions.waiting])).toEqual([["studio", true, 1], ["build-01", true, 0], ["laptop-02", false, 0]]);
    expect(h.hygiene.leftover.count).toBe(3);
  });
  it("manages without the fleet: the local machine by its id, no dots, no leftover count", () => {
    const h = headerOf(fragments.header, null, 0, nothingKept);
    expect(h.machines).toEqual([]);
    expect(h.memory.machine).toBe("studio-id");
    expect(h.hygiene.leftover.count).toBeUndefined();
  });
  it("takes the hour's trend and delta from what the app kept", () => {
    const h = headerOf(fragments.header, data, 0, { ...nothingKept, attention: { waiting: [1, 1, 2, 3], review: [3], stuck: [2, 1] } });
    expect(h.attention.waiting.delta).toBe(2);
    expect(changeWords(h.attention.waiting)).toBe("up 2 in the last hour");
    expect(changeWords(h.attention.stuck)).toBe("down 1 in the last hour");
    expect(changeWords(h.attention.review)).toBe("steady over the last hour");
  });
  it("turns kept polls into strips, once there is more than one", () => {
    const h = headerOf(fragments.header, data, 0, nothingKept);
    expect(h.machines.map(pollOf)).toEqual(["waiting", "stuck", "offline"]);
    expect(stripsOf({ ...nothingKept, polls: { "studio-id": ["working", "waiting"], "build-id": ["stuck"] } }, h.machines)).toMatchObject({ "studio-id": { cells: [{ state: "working" }, { state: "waiting" }], from: "2 polls" } });
    expect(stripsOf({ ...nothingKept, polls: { "build-id": ["stuck"] } }, h.machines)).toEqual({});
  });
});

describe("trendOf", () => {
  it("gives one reading a minute, carrying the last reading forward", () => {
    const counts = (waiting: number) => ({ waiting, review: 0, stuck: 0 });
    const samples = [{ at: storyNow - 3 * 60_000, counts: counts(1) }, { at: storyNow - 2 * 60_000 + 5000, counts: counts(2) }, { at: storyNow, counts: counts(3) }];
    expect(trendOf(samples, "waiting", storyNow)).toEqual([1, 1, 2, 3]);
    expect(trendOf([], "waiting", storyNow)).toEqual([]);
  });
});
