import { describe, expect, it } from "vitest";
import * as panelStories from "./panels/PanelView.stories";
import type { Viz } from "./panels/types";

// Every component the app or the MCP App renders has a story file beside
// it (Canon design.md: the catalog covers the real product). A file listed
// here has none, for the reason given.
const covered: Record<string, string> = {
  "./host/main.tsx": "the page entry; Pages/Home renders App",
  "./mcp-app/main.tsx": "the MCP App entry; Embedded/Panels app renders PanelsView",
  "./panels/host.tsx": "the host context and row actions, which every panel story uses",
  "./theme/Provider.tsx": "the theme provider, which the preview wraps every story in",
};

// One story of each panel kind, by its export in Components/Panel.
const vizStories: Record<Viz, string> = { stat: "Stat", gauge: "Gauge", timeseries: "Timeseries", bar: "Bar", table: "Table", list: "List", inbox: "Inbox", cards: "Cards", timeline: "Timeline", text: "Notes" };

describe("catalog", () => {
  const files = Object.keys(import.meta.glob(["./**/*.tsx", "!./**/*.stories.tsx", "!./**/*.test.tsx", "!./fixtures/**"]));
  const stories = new Set(Object.keys(import.meta.glob("./**/*.stories.tsx")));
  it("has a story file for every component", () => {
    const missing = files.filter(f => !covered[f] && !stories.has(f.replace(/\.tsx$/, ".stories.tsx")));
    expect(missing).toEqual([]);
  });
  it("lists only files that exist as covered", () => {
    expect(Object.keys(covered).filter(f => !files.includes(f))).toEqual([]);
  });
  it("keeps fixtures out of the app: only stories and tests import them", () => {
    const sources = import.meta.glob<string>(["./**/*.{ts,tsx}", "!./**/*.stories.tsx", "!./**/*.test.{ts,tsx}", "!./fixtures/**", "!./host/flows/**"], { query: "?raw", import: "default", eager: true });
    const importing = Object.entries(sources).filter(([, text]) => /(?:from|import)\s*\(?\s*["'][^"']*\/fixtures\//.test(text)).map(([file]) => file);
    expect(importing).toEqual([]);
  });
  it("has a story for every kind of panel", () => {
    const missing = Object.entries(vizStories).filter(([, name]) => !(name in panelStories)).map(([viz]) => viz);
    expect(missing).toEqual([]);
  });
});
