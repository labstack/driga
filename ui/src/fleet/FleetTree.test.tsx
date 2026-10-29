import { MantineProvider } from "@mantine/core";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { FleetTree } from "./FleetTree";
import type { FleetData, FleetSession } from "./FleetTypes";

const session = (id: string, parent?: string, project = "alpha", machine = "local"): FleetSession => ({ id, parent, project, machine, title: `Session ${id}`, state: "waiting", agent: "claude", memory: 100, lastActivity: "2026-03-14T15:00:00Z" });
function render(sessions: FleetSession[]): string {
  const data: FleetData = {
    machines: ["local", "remote"].map(id => ({ id, name: id, reachable: true, lastSeen: "2026-03-14T15:00:00Z", memoryUsed: 100, memoryTotal: 1000 })),
    projects: ["alpha", "beta"].map(id => ({ id, name: id, repo: `/${id}` })),
    sessions,
  };
  return renderToStaticMarkup(<MantineProvider><FleetTree data={data} selected={null} onSelect={() => {}} onOpen={() => {}} onReply={() => {}} /></MantineProvider>);
}
const appearances = (html: string, id: string) => (html.match(new RegExp(`title="Session ${id}"`, "g")) ?? []).length;

describe("FleetTree", () => {
  it("renders subagents at every depth", () => {
    const html = render([session("root"), session("child", "root"), session("grandchild", "child"), session("great-grandchild", "grandchild")]);
    for (const id of ["root", "child", "grandchild", "great-grandchild"]) expect(appearances(html, id)).toBe(1);
    expect(html.indexOf('title="Session child"')).toBeLessThan(html.indexOf('title="Session grandchild"'));
  });
  it("renders a child in another project exactly once, under its parent", () => {
    const html = render([session("root"), session("child", "root", "beta")]);
    expect(appearances(html, "child")).toBe(1);
    expect(html.indexOf('title="Session root"')).toBeLessThan(html.indexOf('title="Session child"'));
  });
  it("keeps a parent on another machine from adopting a local root", () => {
    const html = render([session("parent", undefined, "alpha", "remote"), session("child", "parent")]);
    expect(appearances(html, "parent")).toBe(1);
    expect(appearances(html, "child")).toBe(1);
  });
  it("names session actions by their session title", () => {
    const html = render([session("root")]);
    expect(html).toContain('aria-label="Reply to Session root"');
    expect(html).toContain('aria-label="Open Session root"');
  });
  it("keeps session IDs and full titles separate, and passes evidence to the badge", () => {
    const title = "A long session title that must keep its ID visible";
    const html = render([{ ...session("1028"), title, state: "idle", label: "unknown", inferred: true, evidence: "No agent state was reported" }]);
    expect(html).toContain(`title="${title}"`);
    expect(html).toMatch(/<p[^>]*style="[^"]*flex:none;white-space:nowrap[^"]*"[^>]*>#1028<\/p>/);
    expect(html).toContain('aria-description="No agent state was reported"');
    expect(html).not.toContain("~unknown");
  });
});
