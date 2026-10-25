import { expect, test } from "vitest";
import { filter, sessionActions } from "./Search";

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
