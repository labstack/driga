// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Fragment } from "../panels/types";
import { APIError } from "./api";
import { ChatCard, type Card } from "./ChatCard";

const api = vi.hoisted(() => ({ open: vi.fn(), run: vi.fn() }));
vi.mock("./api", async importOriginal => ({ ...await importOriginal<typeof import("./api")>(), dashboards: api }));
vi.mock("../panels/Board", () => ({ Board: ({ fragment }: { fragment: Fragment }) => <div data-testid="board">{fragment.dashboard.name}</div> }));
const panel = { id: "value", title: "Value", viz: "stat" as const, source: "summary" };
const fragment = (name: string): Fragment => ({ dashboard: { name, panels: [panel] }, results: [{ id: "value", status: "ok", frame: { columns: [{ name: "value", type: "number" }], rows: [[42]] } }], observed_at: "2026-03-14T15:00:00Z", history: { attention: [], polls: [] } });
const card = (name = "Memory"): Card => ({ tool: "show_panels", input: { dashboard: { name, panels: [panel] } } });
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let container: HTMLDivElement;
let root: Root;
let unmounted: boolean;
async function render(value: Card = card(), open = true) {
  await act(async () => { root.render(<MantineProvider><ChatCard text={JSON.stringify(value)} open={open} onOpen={() => {}} /></MantineProvider>); });
}
async function toggle() { await act(async () => container.querySelector<HTMLButtonElement>("button[aria-expanded]")!.click()); }
const board = () => container.querySelector('[data-testid="board"]');

beforeEach(() => {
  vi.clearAllMocks();
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  api.run.mockResolvedValue(fragment("Memory"));
  api.open.mockResolvedValue(fragment("Saved"));
  container = document.createElement("div"); document.body.append(container); root = createRoot(container); unmounted = false;
});
afterEach(async () => { if (!unmounted) await act(async () => root.unmount()); container.remove(); });

describe("ChatCard lifecycle", () => {
  it("follows the newest-card open prop until the user toggles", async () => {
    await render(); expect(board()).not.toBeNull();
    await render(card(), false); expect(board()).toBeNull();
    await toggle(); expect(board()).not.toBeNull();
    await render(card(), false); expect(board()).not.toBeNull();
    await render(card(), true); await toggle(); expect(board()).toBeNull();
    await render(card(), true); expect(board()).toBeNull();
  });
  it("aborts the previous request when its card text changes", async () => {
    const pending = deferred<Fragment>(); api.run.mockReturnValue(pending.promise);
    await render(card("Old"));
    const oldSignal = api.run.mock.calls[0][2] as AbortSignal;
    expect(oldSignal.aborted).toBe(false);
    await render(card("New"));
    expect(oldSignal.aborted).toBe(true);
    expect((api.run.mock.calls[1][2] as AbortSignal).aborted).toBe(false);
  });
  it("aborts a saved-dashboard request on unmount", async () => {
    const pending = deferred<Fragment>(); api.open.mockReturnValue(pending.promise);
    await render({ tool: "open_dashboard", input: { id: "saved" } });
    const signal = api.open.mock.calls[0][2] as AbortSignal;
    expect(signal.aborted).toBe(false);
    await act(async () => root.unmount()); unmounted = true;
    expect(signal.aborted).toBe(true);
  });
  it("ignores a stale successful request even when its transport ignores abort", async () => {
    const old = deferred<Fragment>(); const next = deferred<Fragment>();
    api.run.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise);
    await render(card("Old")); await render(card("New"));
    await act(async () => next.resolve(fragment("New"))); expect(board()?.textContent).toBe("New");
    await act(async () => old.resolve(fragment("Old"))); expect(board()?.textContent).toBe("New");
  });
  it("ignores a stale request error after the new card loaded", async () => {
    const old = deferred<Fragment>(); const next = deferred<Fragment>();
    api.run.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise);
    await render(card("Old")); await render(card("New"));
    await act(async () => next.resolve(fragment("New")));
    await act(async () => old.reject(new Error("old error")));
    expect(board()?.textContent).toBe("New"); expect(container.textContent).not.toContain("old error");
  });
  it("uses the typed 404 status to recognize a deleted dashboard", async () => {
    api.open.mockRejectedValue(new APIError("no dashboard with that id", 404));
    await render({ tool: "open_dashboard", input: { id: "deleted" } });
    expect(container.textContent).toContain("This dashboard was deleted since.");
  });
  it("does not confuse not-found words in other errors with deletion", async () => {
    api.open.mockRejectedValue(new APIError("source not found during an unavailable scan", 503));
    await render({ tool: "open_dashboard", input: { id: "saved" } });
    expect(container.textContent).toContain("source not found during an unavailable scan");
    expect(container.textContent).not.toContain("was deleted since");
  });
  it("keeps an unknown write outcome distinct from a rejected draft and does not refetch", async () => {
    await render({ tool: "create_dashboard", input: { dashboard: { name: "Memory", panels: [panel] } }, status: "unknown", error: "Outcome unknown: no tool result was received" });
    expect(container.textContent).toContain("Outcome unknown: Memory");
    expect(container.textContent).toContain("no tool result was received");
    expect(container.textContent).not.toContain("Draft rejected");
    expect(api.run).not.toHaveBeenCalled(); expect(api.open).not.toHaveBeenCalled();
  });
});
