import { expect, it } from "vitest";
import { fakeService } from "./service";
import { fragments } from "./fragments";

it("serves the catalog and persists header add, remove, move, replace and reset", async () => {
  const service = fakeService();
  const post = async (route: string, body: unknown) => service(`/app/api/${route}`, { body: JSON.stringify(body) });
  const { widgets } = await (await post("catalog", {})).json();
  expect(widgets).toHaveLength(18);
  const cpu = widgets.find((p: { id: string }) => p.id === "cpu");
  const original = await (await post("open", { id: "header" })).json();
  const changed = await (await post("edit", { id: "header", base_version: original.version, operations: [{ op: "remove", id: "reclaim" }, { op: "add", panel: cpu }, { op: "move", id: "cpu", before: "waiting" }] })).json();
  expect(changed.dashboard.panels.map((p: { id: string }) => p.id)).toEqual(["cpu", "waiting", "finished", "stuck", "leftover", "memory"]);
  expect(changed.results.find((r: { id: string }) => r.id === "cpu").status).toBe("ok");
  expect((await (await post("open", { id: "header" })).json()).dashboard).toEqual(changed.dashboard);
  expect((await post("edit", { id: "header", base_version: original.version, operations: [] })).status).toBe(409);
  const reset = await (await post("reset", { id: "header", base_version: changed.version })).json();
  expect(reset.version).toBe(changed.version + 1);
  expect(reset.dashboard).toEqual(fragments.header.dashboard);
  const undone = await (await post("replace", { id: "header", base_version: reset.version, dashboard: changed.dashboard })).json();
  expect(undone.version).toBe(reset.version + 1);
  expect(undone.dashboard).toEqual(changed.dashboard);
  const isolated = await (await fakeService()("/app/api/open", { body: JSON.stringify({ id: "header" }) })).json();
  expect(isolated.dashboard).toEqual(original.dashboard);
});
