// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import type { SpotlightActionData, SpotlightActionGroupData } from "@mantine/spotlight";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, test, vi } from "vitest";
import type { Fragment } from "../panels/types";
import { Search } from "./Search";

const api = vi.hoisted(() => ({ run: vi.fn() }));
vi.mock("./api", () => ({ dashboards: api }));
vi.mock("@mantine/spotlight", async importOriginal => {
  const actual = await importOriginal<typeof import("@mantine/spotlight")>();
  const { useEffect } = await import("react");
  return { ...actual, spotlight: { open: vi.fn() }, Spotlight: ({ actions, onSpotlightOpen }: { actions: (SpotlightActionData | SpotlightActionGroupData)[]; onSpotlightOpen: () => void }) => {
    useEffect(() => { onSpotlightOpen(); }, []);
    const list = actions.flatMap(a => actual.isActionsGroup(a) ? a.actions : [a]);
    return <div>{list.map(a => <button key={a.id} onClick={a.onClick}>{a.label}</button>)}</div>;
  } };
});

test("copying a resume command remains safe when clipboard access is unavailable", async () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  const fragment: Fragment = {
    dashboard: { name: "Search", panels: [] }, observed_at: "2026-03-14T15:00:00Z", history: { attention: [], polls: [] },
    results: [{ id: "ended", status: "ok", frame: { columns: ["id", "title", "resume"].map(name => ({ name, type: "string" })), rows: [["2", "Resume this session", "cd /repo && codex resume recorded-session"]] } }],
  };
  api.run.mockResolvedValue(fragment);
  const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
  const say = vi.fn();
  const errors: unknown[] = [];
  const onError = (event: ErrorEvent) => { errors.push(event.error); event.preventDefault(); };
  window.addEventListener("error", onError);
  const container = document.createElement("div"); document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<MantineProvider><Search list={[]} act={async () => {}} ask={() => {}} open={() => {}} say={say} /></MantineProvider>); });
    const button = [...container.querySelectorAll("button")].find(b => b.textContent === "Resume this session");
    expect(button).toBeDefined();
    await act(async () => { button!.click(); });
    expect(errors).toEqual([]);
    expect(say).toHaveBeenCalledWith(expect.stringContaining("cd /repo && codex resume recorded-session"));
  } finally {
    await act(async () => root.unmount()); container.remove();
    window.removeEventListener("error", onError);
    if (original) Object.defineProperty(navigator, "clipboard", original); else Reflect.deleteProperty(navigator, "clipboard");
  }
});
