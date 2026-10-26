// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import type { SpotlightActionData, SpotlightActionGroupData } from "@mantine/spotlight";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, test, vi } from "vitest";
import type { Fragment } from "../panels/types";
import { filter, Search, sessionActions } from "./Search";

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

const live = [{ id: 1, title: "Fix login", project: "api", branch: "fix/login", agent: "claude", machine: "volt", machine_id: "m", state: "working", last_activity_at: "2026-10-25T10:00:00Z" }];
const ended = [{ id: 2, title: "Fix the login page", project: "web", branch: "fix/login", agent: "codex", machine: "volt", machine_id: "m", ended_at: "2026-10-25T09:00:00Z", resume: "cd '/web' && codex resume 'x'" }];

test("live sessions come first and every query word must match", () => {
  const groups = sessionActions(live, ended, Date.parse("2026-10-25T11:00:00Z"), async () => undefined, () => undefined);
  expect(groups.map(g => g.group)).toEqual(["Live sessions", "Ended sessions"]);
  // "login" matches both, and live still comes first although the ended
  // title matches more of it.
  const both = filter("login", groups) as typeof groups;
  expect(both.map(g => g.group)).toEqual(["Live sessions", "Ended sessions"]);
  const one = filter("login codex", groups) as typeof groups;
  expect(one.map(g => g.group)).toEqual(["Ended sessions"]);
  expect(filter("login nothing", groups)).toEqual([]);
});

test("an ended session resumes and a live one opens its pane", () => {
  const opened: unknown[] = [];
  const resumed: unknown[] = [];
  const groups = sessionActions(live, ended, Date.now(), async a => { opened.push(a.row.session_id); }, r => resumed.push(r.id));
  groups[0].actions[0].onClick?.({} as never);
  groups[1].actions[0].onClick?.({} as never);
  expect(opened).toEqual([1]);
  expect(resumed).toEqual([2]);
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
  const errors: unknown[] = [];
  const onError = (event: ErrorEvent) => { errors.push(event.error); event.preventDefault(); };
  window.addEventListener("error", onError);
  const container = document.createElement("div"); document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<MantineProvider><Search list={[]} act={async () => {}} ask={() => {}} open={() => {}} say={() => {}} /></MantineProvider>); });
    const button = [...container.querySelectorAll("button")].find(b => b.textContent === "Resume this session");
    expect(button).toBeDefined();
    await act(async () => { button!.click(); });
    expect(errors).toEqual([]);
  } finally {
    await act(async () => root.unmount()); container.remove();
    window.removeEventListener("error", onError);
    if (original) Object.defineProperty(navigator, "clipboard", original); else Reflect.deleteProperty(navigator, "clipboard");
  }
});
