// Fragments Driga's own panel engine produced over a fictional fleet (see
// internal/panel/stories_test.go), so stories render what the service sends.
import type { Dashboard, Fragment, Panel, Result } from "../panels/types";
import data from "./fragments.json";

export type FragmentName = keyof typeof data;

/** storyNow is the fixtures' clock: the fleet was observed at this time. */
export const storyNow = Date.parse("2026-03-14T15:00:00Z");

export const fragments = data as unknown as Record<FragmentName, Fragment>;

/** panelOf finds one panel and its result in a fixture. */
export function panelOf(name: FragmentName, id: string): { panel: Panel; result: Result } {
  const f = fragments[name];
  const panel = f.dashboard.panels.find(p => p.id === id);
  const result = f.results.find(r => r.id === id);
  if (!panel || !result) throw new Error(`no panel ${id} in ${name}`);
  return { panel, result };
}

/** fragmentOf makes a fragment of named panels from the "all" fixture, as
 * the service would run a spec Chat wrote with them. */
export function fragmentOf(dashboard: Dashboard): Fragment {
  const all = fragments.all;
  return { ...all, dashboard, results: dashboard.panels.map(p => all.results.find(r => r.id === p.id) ?? { id: p.id, status: "empty", message: "Nothing matches right now." }) };
}
