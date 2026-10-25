// The app is Chat, with the few numbers that say whether anything needs you
// above it. Dashboards exist only when Chat makes them; opening one docks
// Chat beside it. Every action goes through the app's own checks and dialogs.
import { StrictMode, useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { value } from "../panels/format";
import { HostContext, type Act, type Host } from "../panels/host";
import type { Dashboard as Spec, Fragment, Panel, Prompt, Result } from "../panels/types";
import "../panels/theme.css";
import "../panels/panels.css";
import "./host.css";
import { action, chat, confirm, dashboards, type Operation, type Summary } from "./api";
import { Chat } from "./Chat";
import { Dashboard } from "./Dashboard";

const refreshMs = 5000;
const headerID = "header";

// The brief reads the whole inbox, whatever dashboard is open.
const inboxSpec: Spec = { name: "Inbox", panels: [{ id: "inbox", title: "Inbox", viz: "inbox", source: "inbox" }] };

function initial(): string | null {
  return new URLSearchParams(location.search).get("dashboard");
}

type Metric = { panel: Panel; result?: Result };

function Metrics({ fragment, editing, onAsk, onRemove }: { fragment: Fragment | null; editing: boolean; onAsk: (m: Metric) => void; onRemove: (id: string) => void }) {
  if (!fragment) return <div className="metrics" />;
  return <ul className="metrics" aria-label="Metrics">
    {fragment.dashboard.panels.map(p => {
      const r = fragment.results.find(x => x.id === p.id);
      const n = r?.frame?.rows[0]?.[0];
      const shown = typeof n === "number" ? value(n, r!.frame!.columns[0]?.unit) : "–";
      const hot = typeof n === "number" && n > 0 && (p.id === "waiting" || p.id === "stuck");
      return <li key={p.id} className={hot ? "metric hot" : "metric"}>
        <button type="button" title={p.description ?? `Ask Chat about ${p.title}`} onClick={() => onAsk({ panel: p, result: r })}>
          <span className="metric-value">{shown}</span><span className="metric-label">{p.title}</span>
        </button>
        {editing && <button type="button" className="metric-remove" aria-label={`Remove ${p.title}`} onClick={() => onRemove(p.id)}>×</button>}
      </li>;
    })}
  </ul>;
}

function App() {
  const [id, setId] = useState<string | null>(initial);
  const [list, setList] = useState<Summary[]>([]);
  const [header, setHeader] = useState<Fragment | null>(null);
  const [inbox, setInbox] = useState<Fragment | null>(null);
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [vars, setVars] = useState<Record<string, string>>({});
  const [editing, setEditing] = useState(false);
  const [chatOpen, setChatOpen] = useState(true);
  const [notice, setNotice] = useState<string | null>(null);
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [ask, setAsk] = useState<{ text: string; at: number } | null>(null);
  const timer = useRef<number | undefined>(undefined);

  const say = (text: string) => { setNotice(text); window.setTimeout(() => setNotice(n => n === text ? null : n), 4000); };
  const load = useCallback(async () => {
    const [h, i] = await Promise.allSettled([dashboards.open(headerID), dashboards.run(inboxSpec)]);
    if (h.status === "fulfilled") setHeader(h.value);
    if (i.status === "fulfilled") setInbox(i.value);
    chat.prompts().then(setPrompts, () => setPrompts([]));
    if (!id) { setFragment(null); return; }
    try { setFragment(await dashboards.open(id, vars)); }
    catch (e) { setNotice((e as Error).message); setId(null); }
  }, [id, vars]);
  useEffect(() => { void load(); }, [load]);
  // Refresh while visible; editing holds the board still.
  useEffect(() => {
    if (editing) return;
    const tick = () => { if (document.visibilityState === "visible") void load(); };
    timer.current = window.setInterval(tick, refreshMs);
    return () => window.clearInterval(timer.current);
  }, [load, editing]);
  useEffect(() => { dashboards.list().then(l => setList(l.filter(d => d.id !== headerID)), () => undefined); }, [fragment?.version, fragment?.id]);
  useEffect(() => { history.replaceState(null, "", id ? `/app/?dashboard=${encodeURIComponent(id)}` : "/app/"); }, [id]);

  const open = (next: string | null) => { setEditing(false); setVars({}); setId(next); if (next) setChatOpen(true); };
  const edit = async (target: string, base: number, operations: Operation[], message?: string) => {
    try {
      const next = await dashboards.edit(target, operations, base, target === headerID ? undefined : vars, message);
      if (target === headerID) setHeader(next); else setFragment(next);
    } catch (e) { say((e as Error).message); void load(); }
  };
  const act: Act = async ({ kind, row }) => {
    const machine = row.machine_id;
    const session = Number(row.session_id ?? row.id);
    const name = String(row.title ?? row.signature ?? row.project ?? row.path ?? row.id ?? "");
    // Anything that changes the machine asks the app's dialog first, as the
    // TUI does; the server refuses it otherwise.
    const ask = async (message: string) => { const ok = await confirm(message); if (!ok) say("Cancelled"); return ok; };
    try {
      switch (kind) {
        case "jump":
        case "open": await action({ action: "jump", session_id: session, machine }); say("Opened the session's pane"); return;
        case "ack": await action({ action: "ack_inbox", id: row.id, machine }); say("Acknowledged"); break;
        case "stop":
          if (!await ask(`Stop ${name}? Its agent and the processes it started are ended.`)) return;
          await action({ action: "kill", session_id: session, machine, confirmed: true }); say(`Stopped ${name}`); break;
        case "kill":
        case "force_kill": {
          const force = kind === "force_kill";
          if (!await ask(`${force ? "Force kill" : "Kill"} the ${name} process tree? ${force ? "Processes get SIGKILL without a chance to clean up." : "Processes get SIGTERM first."}`)) return;
          await action({ action: "kill", id: row.id, machine, force, confirmed: true }); say(`Killed ${name}`); break;
        }
        case "cleanup":
          if (!await ask(`Remove ${name}? Driga checks it again before removing it.`)) return;
          await action({ action: "cleanup", id: row.id, machine, confirmed: true }); say(`Removed ${name}`); break;
        case "summarize": await action({ action: "summarize", project: row.path, machine }); say(`Summarizing ${name}`); return;
      }
      void load();
    } catch (e) { say((e as Error).message); }
  };
  const answer = async (prompt: Prompt, allow: boolean) => {
    try { await chat.answer(prompt.id, allow); setPrompts(ps => ps.filter(p => p.id !== prompt.id)); say(allow ? `Allowed ${prompt.tool}` : `Denied ${prompt.tool}`); }
    catch (e) { say((e as Error).message); }
  };
  // A chat card's panels join the open dashboard, or start a new one.
  const pin = async (panels: Panel[], name: string) => {
    if (fragment?.id) {
      const taken = new Set(fragment.dashboard.panels.map(p => p.id));
      const ops: Operation[] = panels.map(p => {
        let pid = p.id, n = 2;
        while (taken.has(pid)) pid = `${p.id}_${n++}`;
        taken.add(pid);
        return { op: "add", panel: { ...p, id: pid, grid: undefined } };
      });
      await edit(fragment.id, fragment.version ?? 0, ops, "Added from Chat");
      say(`Added to ${fragment.dashboard.name}`);
      return;
    }
    try {
      const created = await dashboards.create({ name, panels: panels.map(p => ({ ...p, grid: undefined })) }, "Saved from Chat");
      open(created.id ?? null);
      say(`Saved ${name}`);
    } catch (e) { say((e as Error).message); }
  };
  const remove = async () => {
    if (!fragment?.id) return;
    const name = fragment.dashboard.name;
    if (!await confirm(`Delete the ${name} dashboard and its history?`)) { say("Cancelled"); return; }
    try { await dashboards.delete(fragment.id); open(null); say(`Deleted ${name}`); } catch (e) { say((e as Error).message); }
  };
  const host: Host = { act, local: (fragment ?? header)?.local_machine, prompts, answer };

  // The app's menus drive the page through drigaCommand.
  useEffect(() => {
    (window as unknown as { drigaCommand: (name: string, arg?: unknown) => void }).drigaCommand = name => {
      if (name === "reload") void load();
      else if (name === "back") history.back();
      else if (name === "forward") history.forward();
      else if (name === "openSession") open(null);
    };
  }, [load]);

  // The brief's status line repeats the header's nonzero numbers.
  const status = (header?.dashboard.panels ?? []).flatMap(p => {
    const r = header!.results.find(x => x.id === p.id);
    const n = r?.frame?.rows[0]?.[0];
    if (typeof n !== "number" || n === 0) return [];
    return [`${p.title}: ${value(n, r!.frame!.columns[0]?.unit)}`];
  });
  const inboxPanel = inbox?.dashboard.panels[0];
  const brief = inboxPanel ? { panel: inboxPanel, result: inbox!.results[0] } : undefined;
  const docked = !!fragment;
  return <HostContext.Provider value={host}><div className={docked ? (chatOpen ? "shell docked with-chat" : "shell docked") : "shell solo"}>
    <header className="top">
      <span className="mark" aria-hidden="true"><svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="none" stroke="#fff" strokeWidth="1.6" /></svg></span>
      <Metrics fragment={header} editing={editing && !docked} onAsk={m => setAsk({ text: `Tell me about "${m.panel.title}" right now.`, at: Date.now() })}
        onRemove={pid => header && void edit(headerID, header.version ?? 0, [{ op: "remove", id: pid }], "Removed a metric")} />
      <span className="spacer" />
      {list.length > 0 && <label className="picker"><span className="sr-only">Dashboard</span>
        <select value={id ?? ""} onChange={e => open(e.target.value || null)}>
          <option value="">Chat</option>
          {list.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
        </select>
      </label>}
      {fragment?.dashboard.variables?.map(v => <label key={v.name} className="picker"><span>{v.name}</span>
        <select value={vars[v.name] ?? ""} onChange={e => setVars(prev => ({ ...prev, [v.name]: e.target.value }))}>
          <option value="">All</option>
          {(fragment.options?.[v.name] ?? []).map(o => <option key={o} value={o}>{o}</option>)}
        </select>
      </label>)}
      <button type="button" aria-pressed={editing} onClick={() => setEditing(e => !e)}>{editing ? "Done" : "Edit"}</button>
      {docked && <button type="button" aria-pressed={chatOpen} onClick={() => setChatOpen(o => !o)}>Chat</button>}
    </header>
    {docked && <main className="main">
      {editing && <div className="hint"><span className="muted">Drag panels by their title, resize from the corner, or ask Chat to change them.</span><button type="button" className="danger" onClick={() => void remove()}>Delete dashboard</button></div>}
      <Dashboard fragment={fragment!} editing={editing} act={act}
        onLayout={grids => void edit(fragment!.id!, fragment!.version ?? 0, [{ op: "layout", grids }], "Moved panels")}
        onRemove={pid => void edit(fragment!.id!, fragment!.version ?? 0, [{ op: "remove", id: pid }], "Removed a panel")} />
    </main>}
    {editing && !docked && <p className="hint muted solo-hint">Remove a metric with ×, or ask Chat to add one, like “add cost today to the header”.</p>}
    {(!docked || chatOpen) && <Chat inbox={brief} status={status} act={act} ask={ask} pinLabel={fragment ? `Add to ${fragment.dashboard.name}` : "Save as dashboard"} onPin={pin} onClose={docked ? () => setChatOpen(false) : undefined} />}
    {notice && <p className="toast" role="status">{notice}</p>}
  </div></HostContext.Provider>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><App /></StrictMode>);
