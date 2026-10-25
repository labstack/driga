// The app's home: a dashboard the user owns, with Chat docked beside it.
// Every action goes through the app's own checks and dialogs.
import { StrictMode, useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { value } from "../panels/format";
import type { Act } from "../panels/PanelView";
import type { Fragment, Panel } from "../panels/types";
import "../panels/theme.css";
import "../panels/panels.css";
import "./host.css";
import { action, dashboards, type Operation, type Summary } from "./api";
import { Chat } from "./Chat";
import { Dashboard } from "./Dashboard";

const refreshMs = 5000;

function initial(): string {
  return new URLSearchParams(location.search).get("dashboard") || "home";
}

function App() {
  const [id, setId] = useState(initial);
  const [list, setList] = useState<Summary[]>([]);
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [vars, setVars] = useState<Record<string, string>>({});
  const [editing, setEditing] = useState(false);
  const [chatOpen, setChatOpen] = useState(true);
  const [notice, setNotice] = useState<string | null>(null);
  const timer = useRef<number | undefined>(undefined);

  const load = useCallback(async () => {
    try { setFragment(await dashboards.open(id, vars)); }
    catch (e) { setNotice((e as Error).message); }
  }, [id, vars]);
  useEffect(() => { void load(); }, [load]);
  // Refresh while visible; editing holds the board still.
  useEffect(() => {
    if (editing) return;
    const tick = () => { if (document.visibilityState === "visible") void load(); };
    timer.current = window.setInterval(tick, refreshMs);
    return () => window.clearInterval(timer.current);
  }, [load, editing]);
  useEffect(() => { dashboards.list().then(setList, () => undefined); }, [fragment?.version]);
  useEffect(() => { history.replaceState(null, "", id === "home" ? "/app/" : `/app/?dashboard=${encodeURIComponent(id)}`); }, [id]);

  const say = (text: string) => { setNotice(text); window.setTimeout(() => setNotice(n => n === text ? null : n), 4000); };
  const edit = async (operations: Operation[], message?: string) => {
    if (!fragment) return;
    try { setFragment(await dashboards.edit(id, operations, fragment.version ?? 0, vars, message)); }
    catch (e) { say((e as Error).message); void load(); }
  };
  const act: Act = async ({ kind, row }) => {
    try {
      if (kind === "jump" || kind === "open") { await action({ action: "jump", session_id: Number(row.session_id ?? row.id), machine: row.machine_id }); say("Opened the session's pane"); }
      if (kind === "ack") { await action({ action: "ack_inbox", id: row.id, machine: row.machine_id }); say("Acknowledged"); void load(); }
    } catch (e) { say((e as Error).message); }
  };
  const pin = (panels: Panel[]) => {
    const taken = new Set(fragment?.dashboard.panels.map(p => p.id));
    const ops: Operation[] = panels.map(p => {
      let pid = p.id, n = 2;
      while (taken.has(pid)) pid = `${p.id}_${n++}`;
      taken.add(pid);
      return { op: "add", panel: { ...p, id: pid, grid: undefined } };
    });
    void edit(ops, "Added from Chat");
    say(`Added ${panels.length === 1 ? "a panel" : `${panels.length} panels`} to ${fragment?.dashboard.name ?? "the dashboard"}`);
  };

  // The app's menus drive the page through drigaCommand, as on every page.
  useEffect(() => {
    // Every view is a dashboard: menu paths map onto the built-in ones.
    const open = (dashboard: string) => { setEditing(false); setVars({}); setId(dashboard); };
    const forPath = (path: string) => path.startsWith("/machines") ? "machines" : path.startsWith("/projects") ? "projects" : path.startsWith("/flow") ? "flow" : "home";
    (window as unknown as { drigaCommand: (name: string, arg?: unknown) => void }).drigaCommand = (name, arg) => {
      if (name === "reload") void load();
      else if (name === "back") history.back();
      else if (name === "forward") history.forward();
      else if (name === "view" && arg === "chat") setChatOpen(o => !o);
      else if (name === "view") open(String(arg));
      else if (name === "go") open(forPath(String(arg)));
      else if (name === "openSession") { open("flow"); setChatOpen(true); }
    };
  }, [load]);

  // The brief's one-line status, from the stat panels the dashboard shows.
  const status = (fragment?.dashboard.panels ?? []).filter(p => p.viz === "stat").flatMap(p => {
    const r = fragment!.results.find(x => x.id === p.id);
    const n = r?.frame?.rows[0]?.[0];
    if (typeof n !== "number" || n === 0) return [];
    return [`${p.title}: ${value(n, r!.frame!.columns[0]?.unit)}`];
  });
  const inboxPanel = fragment?.dashboard.panels.find(p => p.viz === "inbox");
  const inbox = inboxPanel ? { panel: inboxPanel, result: fragment!.results.find(r => r.id === inboxPanel.id) } : undefined;
  return <div className={chatOpen ? "shell with-chat" : "shell"}>
    <header className="top">
      <span className="mark" aria-hidden="true"><svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="none" stroke="#fff" strokeWidth="1.6" /></svg></span>
      <strong className="wordmark">driga</strong>
      <nav aria-label="Dashboards" className="tabs">
        {(list.length ? list : [{ id: "home", name: "Home", panels: 0, version: 0 }]).map(d => <button type="button" key={d.id} aria-current={d.id === id ? "page" : undefined} onClick={() => { setEditing(false); setVars({}); setId(d.id); }}>{d.name}</button>)}
      </nav>
      {fragment?.dashboard.variables?.map(v => <label key={v.name} className="picker"><span>{v.name}</span>
        <select value={vars[v.name] ?? ""} onChange={e => setVars(prev => ({ ...prev, [v.name]: e.target.value }))}>
          <option value="">All</option>
          {(fragment.options?.[v.name] ?? []).map(o => <option key={o} value={o}>{o}</option>)}
        </select>
      </label>)}
      <span className="spacer" />
      <button type="button" aria-pressed={editing} onClick={() => setEditing(e => !e)}>{editing ? "Done" : "Edit"}</button>
      <button type="button" aria-pressed={chatOpen} onClick={() => setChatOpen(o => !o)}>Chat</button>
    </header>
    <main className="main">
      {editing && <p className="hint muted">Drag panels by their title, resize from the corner, or ask Chat to add one.</p>}
      {fragment ? <Dashboard fragment={fragment} editing={editing} act={act}
        onLayout={grids => void edit([{ op: "layout", grids }], "Moved panels")}
        onRemove={pid => void edit([{ op: "remove", id: pid }], "Removed a panel")} /> : <p className="muted">Loading…</p>}
    </main>
    {chatOpen && <Chat inbox={inbox} status={status} act={act} onPin={pin} onClose={() => setChatOpen(false)} />}
    {notice && <p className="toast" role="status">{notice}</p>}
  </div>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><App /></StrictMode>);
