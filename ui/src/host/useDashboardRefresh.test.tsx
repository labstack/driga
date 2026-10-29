// @vitest-environment happy-dom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Fragment, Prompt } from "../panels/types";
import { useDashboardRefresh } from "./useDashboardRefresh";

const api = vi.hoisted(() => ({ open: vi.fn(), run: vi.fn(), list: vi.fn(), prompts: vi.fn(), bursts:vi.fn(), grantHistory: vi.fn().mockResolvedValue([]), edit: vi.fn() }));
vi.mock("./api", () => ({ dashboards: api, chat: { prompts: api.prompts, grantHistory: api.grantHistory, bursts: api.bursts } }));
const fragment = (id: string, version = 1): Fragment => ({ id, version, dashboard: { name: id, panels: [] }, results: [], history: { attention: [], polls: [] }, observed_at: "2026-03-14T15:00:00Z" });
const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(yes => { resolve = yes; });
  return { promise, resolve };
};
let root: Root;
let container: HTMLDivElement;
let state: ReturnType<typeof useDashboardRefresh>;
const onError = vi.fn(), onMissing = vi.fn();
const vars = {};
function Harness({ id = "home", variables = vars }: { id?: string; variables?: Record<string, string> }) {
  state = useDashboardRefresh({ id, vars: variables, onError, onMissing });
  return null;
}
const mount = (id = "home", variables = vars) => act(async () => { root.render(<Harness id={id} variables={variables} />); });

beforeEach(() => {
  vi.resetAllMocks(); api.grantHistory.mockResolvedValue([]);
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  api.open.mockImplementation((id: string) => Promise.resolve(fragment(id)));
  api.run.mockImplementation((d: { name: string }) => Promise.resolve(fragment(d.name)));
  api.prompts.mockResolvedValue([]); api.bursts.mockResolvedValue([]);
  api.list.mockResolvedValue([]);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });

describe("dashboard refresh publications", () => {
  it("retains failed supplements independently and tracks selected freshness", async () => {
    const prompts = [{ id: "pending" }] as Prompt[];
    api.prompts.mockResolvedValue(prompts);
    await mount();
    const inbox = state.inbox, fleet = state.fleet, at = state.fresh.at;
    api.open.mockImplementation((id: string) => id === "header" ? Promise.resolve(fragment("new header")) : Promise.reject(new Error("offline")));
    api.run.mockRejectedValueOnce(new Error("inbox failed")).mockResolvedValueOnce(fragment("new fleet"));
    api.prompts.mockRejectedValueOnce(new Error("prompt failure"));
    await act(async () => { await state.refresh(); });
    expect(state.header?.id).toBe("new header");
    expect(state.inbox).toBe(inbox);
    expect(state.fleet).not.toBe(fleet);
    expect(state.fleet?.id).toBe("new fleet");
    expect(state.prompts).toEqual(prompts);
    expect(state.fragment?.id).toBe("home");
    expect(state.fresh).toEqual({ at, failed: true });
    expect(onMissing).not.toHaveBeenCalled();
  });

  it("rejects a response for old variable values", async () => {
    const old = deferred<Fragment>();
    api.open.mockImplementation((id: string, values?: Record<string, string>) => values?.project === "old" ? old.promise : Promise.resolve(fragment(id)));
    await mount("home", { project: "old" });
    await mount("home", { project: "new" });
    await act(async () => { old.resolve(fragment("old variables")); });
    expect(state.fragment?.id).toBe("home");
  });

  it("clears a previous visit when returning to the same dashboard", async () => {
    await mount();
    const pending = deferred<Fragment>();
    api.open.mockImplementation((id: string) => id === "other" || id === "home" ? pending.promise : Promise.resolve(fragment(id)));
    await mount("other");
    await mount("home");
    expect(state.fragment).toBeNull();
    expect(state.fresh).toEqual({ at: 0, failed: false });
    await act(async () => { pending.resolve(fragment("home", 2)); });
    expect(state.fragment?.version).toBe(2);
  });

  it("invalidates an in-flight refresh on entering editing", async () => {
    await mount();
    const old = deferred<Fragment>();
    api.open.mockImplementation((id: string) => id === "home" ? old.promise : Promise.resolve(fragment(id)));
    await act(async () => { void state.refresh(); });
    await act(async () => { state.setEditing(true); });
    await act(async () => { old.resolve(fragment("stale")); });
    expect(state.fragment?.id).toBe("home");
  });

  it("allows explicit reloads and variable changes while editing", async () => {
    await mount();
    await act(async () => { state.setEditing(true); });
    api.open.mockImplementation((id: string) => Promise.resolve(fragment(id, 2)));
    await act(async () => { await state.refresh(); });
    expect(state.fragment?.version).toBe(2);
    api.open.mockImplementation((id: string) => Promise.resolve(fragment(id, 3)));
    await mount("home", { project: "new" });
    expect(state.fragment?.version).toBe(3);
  });

  it("does not let a poll overwrite a committed edit or retry a warning outcome", async () => {
    await mount();
    const old = deferred<Fragment>(), save = deferred<Fragment>();
    api.open.mockImplementation((id: string) => id === "home" ? old.promise : Promise.resolve(fragment(id)));
    api.edit.mockReturnValue(save.promise);
    await act(async () => { void state.refresh(); });
    await act(async () => { void state.edit("home", 1, [{ op: "rename", name: "Saved" }]); });
    const calls = api.open.mock.calls.length;
    await act(async () => { await state.refresh(); });
    expect(api.open).toHaveBeenCalledTimes(calls);
    const saved: Fragment = { ...fragment("home", 2), dashboard: { name: "Saved", panels: [] }, results: [{ id: "warning", status: "error", message: "Saved dashboard; live data unavailable" }] };
    await act(async () => { save.resolve(saved); });
    await act(async () => { old.resolve(fragment("stale", 1)); });
    expect(state.fragment).toEqual(saved);
    expect(state.fresh.failed).toBe(false);
    expect(api.edit).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
  });

  it("lets a new scope refresh while an old edit is pending", async () => {
    await mount();
    const save = deferred<Fragment>(), next = deferred<Fragment>();
    api.edit.mockReturnValue(save.promise);
    await act(async () => { void state.edit("home", 1, []); });
    api.open.mockImplementation((id: string) => id === "other" ? next.promise : Promise.resolve(fragment(id)));
    await mount("other");
    await act(async () => { save.resolve(fragment("home", 2)); });
    await act(async () => { next.resolve(fragment("other")); });
    expect(state.fragment?.id).toBe("other");
    expect(onError).not.toHaveBeenCalled();
  });
});

it("publishes a header edit immediately and discards an older poll", async () => {
  await mount();
  const old = deferred<Fragment>(), save = deferred<Fragment>();
  api.open.mockImplementation((id: string) => id === "header" ? old.promise : Promise.resolve(fragment(id)));
  await act(async () => { void state.refresh(); });
  await act(async () => { void state.mutateHeader(() => save.promise); });
  await act(async () => { save.resolve(fragment("header", 2)); });
  await act(async () => { old.resolve(fragment("header", 1)); });
  expect(state.header?.version).toBe(2);
});
it("finishes a pending header edit across navigation and loads the new page", async () => {
  await mount();
  const save = deferred<Fragment>();
  await act(async () => { void state.mutateHeader(() => save.promise); });
  await mount("other");
  api.open.mockImplementation((id: string) => Promise.resolve(fragment(id, 2)));
  await act(async () => { save.resolve(fragment("header", 2)); });
  expect(state.header?.version).toBe(2);
  expect(state.fragment?.id).toBe("other");
});
