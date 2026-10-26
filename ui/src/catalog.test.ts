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

// Stories whose component is not one the app renders, and why.
const specimens: Record<string, string> = {
  "theme-tokens": "a sample of the theme's tokens, drawn with Mantine in the app's theme",
};

// One story of each panel kind, by its export in Components/Panel.
const vizStories: Record<Viz, string> = { stat: "Stat", gauge: "Gauge", timeseries: "Timeseries", bar: "Bar", table: "Table", list: "List", inbox: "Inbox", cards: "Cards", timeline: "Timeline", text: "Notes" };

// What only stories, tests and fixtures may import: the fixtures folder and
// a component's own <Name>-fixtures module.
const fixtureImport = /(?:from|import)\s*\(?\s*["'][^"']*(?:\/fixtures\/|-fixtures)["']/;

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
  it("keeps fixtures out of the app: only stories, tests and fixtures import them", () => {
    const sources = import.meta.glob<string>(["./**/*.{ts,tsx}", "!./**/*.stories.tsx", "!./**/*.test.{ts,tsx}", "!./**/*-fixtures.ts", "!./fixtures/**", "!./host/flows/**"], { query: "?raw", import: "default", eager: true });
    const importing = Object.entries(sources).filter(([, text]) => fixtureImport.test(text)).map(([file]) => file);
    expect(importing).toEqual([]);
  });
  it("renders the app's own components: each story's component is an app export", () => {
    // The entries mount the page when imported, so they stay out.
    const app = import.meta.glob<Record<string, unknown>>(["./**/*.tsx", "!./**/*.stories.tsx", "!./**/*.test.tsx", "!./fixtures/**", "!./**/main.tsx"], { eager: true });
    const exported = new Set(Object.values(app).flatMap(m => Object.values(m)));
    const stories = import.meta.glob<{ default: { id: string; component?: unknown } }>("./**/*.stories.tsx", { eager: true });
    const foreign = Object.entries(stories)
      .filter(([, m]) => !specimens[m.default.id] && !exported.has(m.default.component))
      .map(([file, m]) => `${file} (${m.default.id})`);
    expect(foreign).toEqual([]);
  });
  it("lists only specimens that are stories", () => {
    const ids = Object.values(import.meta.glob<{ default: { id: string } }>("./**/*.stories.tsx", { eager: true })).map(m => m.default.id);
    expect(Object.keys(specimens).filter(id => !ids.includes(id))).toEqual([]);
  });
  it("keeps drafts out of the app: only drafts and stories import them", () => {
    const sources = import.meta.glob<string>(["./**/*.{ts,tsx}", "!./drafts/**", "!./**/*.stories.tsx", "!./**/*.test.{ts,tsx}"], { query: "?raw", import: "default", eager: true });
    const importing = Object.entries(sources).filter(([, text]) => /(?:from|import)\s*\(?\s*["'][^"']*\/drafts\//.test(text)).map(([file]) => file);
    expect(importing).toEqual([]);
  });
  it("has a story for every kind of panel", () => {
    const missing = Object.entries(vizStories).filter(([, name]) => !(name in panelStories)).map(([viz]) => viz);
    expect(missing).toEqual([]);
  });
});
