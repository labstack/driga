// The MCP App: a chat host renders it for every Driga panel tool result.
// It shows the fragment, and re-runs it through run_panels when a variable
// changes. It runs no actions: a chat's own tools do that, with approval.
import type { App, McpUiHostContext } from "@modelcontextprotocol/ext-apps";
import { useApp } from "@modelcontextprotocol/ext-apps/react";
import { StrictMode, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { Board } from "../panels/Board";
import { isFragment, normalize, type Fragment } from "../panels/types";
import "../panels/theme.css";
import "../panels/panels.css";

type ToolResult = Awaited<ReturnType<App["callServerTool"]>>;

function fragmentOf(result: ToolResult): Fragment | string {
  if (result.isError) {
    const text = result.content?.find(c => c.type === "text");
    return text && "text" in text ? text.text : "This view could not be loaded.";
  }
  return isFragment(result.structuredContent) ? normalize(result.structuredContent) : "This result is not a Driga panel.";
}

function PanelsApp() {
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [host, setHost] = useState<McpUiHostContext>();
  const accept = (result: ToolResult) => {
    const next = fragmentOf(result);
    if (typeof next === "string") setProblem(next);
    else { setFragment(next); setProblem(null); }
  };
  const { app, error } = useApp({
    appInfo: { name: "Driga panels", version: "1.0.0" }, capabilities: {}, autoResize: false,
    onAppCreated: created => {
      created.ontoolresult = accept;
      created.ontoolcancelled = () => setProblem("Cancelled.");
      created.onhostcontextchanged = context => setHost(previous => ({ ...previous, ...context }));
    },
  });
  useEffect(() => { if (app) setHost(app.getHostContext()); }, [app]);
  useEffect(() => {
    if (host?.theme) document.documentElement.dataset.theme = host.theme;
  }, [host?.theme]);
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!app || !root.current) return;
    let last = 0;
    const observer = new ResizeObserver(() => {
      const height = Math.round(root.current!.getBoundingClientRect().height);
      if (Math.abs(height - last) >= 2) { last = height; void app.sendSizeChanged({ height }); }
    });
    observer.observe(root.current);
    return () => observer.disconnect();
  }, [app]);
  const onVars = app && fragment ? async (vars: Record<string, string>) => {
    try { accept(await app.callServerTool({ name: "run_panels", arguments: { dashboard: fragment.dashboard, vars } })); }
    catch { setProblem("This view could not be refreshed."); }
  } : undefined;
  return <div ref={root} role="main" aria-label="Driga panels">
    {problem && <p className="problem" role="alert" style={{ padding: "10px 16px", margin: 0 }}>{problem}</p>}
    {fragment ? <Board fragment={fragment} onVars={onVars} compact /> : !problem && <p className="muted" style={{ padding: 16 }}>{error ? "This view could not connect." : "Loading…"}</p>}
  </div>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><PanelsApp /></StrictMode>);
