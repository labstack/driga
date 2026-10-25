import { useEffect, useRef, useState } from "react";
import { Board } from "../panels/Board";
import { useHost } from "../panels/host";
import { PanelView, type Act } from "../panels/PanelView";
import type { Dashboard, Fragment, Panel, Result } from "../panels/types";
import { chat, dashboards, type ChatAction, type ChatMessage, type ChatState } from "./api";

type Card = { tool: string; input: { id?: string; dashboard?: Dashboard; vars?: Record<string, string> } };

/** PanelCard runs a panel tool's input again, so the card shows live data
 * rather than what the model saw. */
function PanelCard({ text, pinLabel, onPin, onSave }: { text: string; pinLabel: string; onPin: (panels: Panel[]) => void; onSave: (panels: Panel[], name: string) => void }) {
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let card: Card;
    try { card = JSON.parse(text) as Card; } catch { setProblem("This card could not be read."); return; }
    const spec = card.input?.dashboard;
    const id = card.input?.id;
    if (!spec && !id) { setProblem("This card names no dashboard."); return; }
    const run = spec && (card.tool === "show_panels" || card.tool === "create_dashboard" || card.tool === "replace_dashboard") ? dashboards.run(spec, card.input.vars) : dashboards.open(id!, card.input?.vars);
    run.then(setFragment, (e: Error) => setProblem(e.message));
  }, [text]);
  if (problem) return <p className="problem">{problem}</p>;
  if (!fragment) return <p className="muted">Loading panel…</p>;
  return <div className="chat-card">
    <Board fragment={fragment} compact />
    <div className="card-actions"><button type="button" onClick={() => onPin(fragment.dashboard.panels)}>{pinLabel}</button><button type="button" onClick={() => onSave(fragment.dashboard.panels, fragment.dashboard.name)}>Save as dashboard</button></div>
  </div>;
}

function ActionCard({ conversation, action }: { conversation: string; action: ChatAction }) {
  const pending = action.status === "pending";
  return <div className={pending ? "chat-action pending" : "chat-action"}>
    <div><strong>{action.title}</strong><span className="muted"> · {action.status}</span></div>
    {action.result && <div className="muted">{action.result}</div>}
    {pending && <div className="card-actions">
      <button type="button" className="primary-button" onClick={() => void chat.decide(conversation, action.id, true)}>Allow</button>
      <button type="button" onClick={() => void chat.decide(conversation, action.id, false)}>Deny</button>
    </div>}
  </div>;
}

/** Brief is what Chat opens with: the inbox and the next steps, posted as if
 * by an assistant that watched while you were away. Nothing runs until you
 * pick something. */
function Brief({ inbox, act, ask }: { inbox?: { panel: Panel; result?: Result; listed: boolean }; act?: Act; ask: (text: string) => void }) {
  const waits = inbox?.result?.frame?.rows.filter(r => r[inbox.result!.frame!.columns.findIndex(c => c.name === "kind")] === "waiting").length ?? 0;
  const { prompts = [] } = useHost();
  const items = (inbox?.result?.frame?.rows.length ?? 0) + prompts.length;
  const suggestions = [
    waits ? "What needs me first, and why?" : "Is anything stuck or looping?",
    "What finished since I was away, and is it safe to merge?",
    "Make a dashboard for the project I am working on",
    "Which processes can I reclaim?",
  ];
  return <div className="brief">
    <p className="brief-lead">{prompts.length > 0 && `${prompts.length} permission ${prompts.length === 1 ? "prompt" : "prompts"} to answer. `}{waits ? `${waits} ${waits === 1 ? "session needs" : "sessions need"} you.` : prompts.length ? "" : "Nothing is waiting on you."}</p>
    {inbox && !inbox.listed && items > 0 && <PanelView panel={inbox.panel} result={inbox.result} act={act} />}
    <div className="chips">{suggestions.map(s => <button type="button" key={s} onClick={() => ask(s)}>{s}</button>)}</div>
  </div>;
}

export function Chat({ inbox, act, ask, pinLabel, onPin, onSave, onClose }: {
  inbox?: { panel: Panel; result?: Result; listed: boolean }; act?: Act;
  /** ask puts a question in the compose box for the user to send. */
  ask: { text: string; at: number } | null;
  pinLabel: string; onPin: (panels: Panel[]) => void; onSave: (panels: Panel[], name: string) => void; onClose?: () => void;
}) {
  const [id, setId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [actions, setActions] = useState<Map<string, ChatAction>>(new Map());
  const [active, setActive] = useState(false);
  const [draft, setDraft] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const log = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (ask) { setDraft(ask.text); input.current?.focus(); } }, [ask]);

  useEffect(() => {
    chat.history().then(list => { if (list.length) setId(list[0].id); }, () => undefined);
  }, []);
  useEffect(() => {
    if (!id) return;
    const control = new AbortController();
    let cursor = 0;
    const byId = new Map<string, ChatMessage>();
    const flush = () => setMessages([...byId.values()].sort((a, b) => a.created_at.localeCompare(b.created_at)));
    const receive = (s: ChatState) => {
      if (s.reset) { byId.clear(); s.messages.forEach(m => byId.set(m.id, m)); setActions(new Map(s.actions.map(a => [a.id, a]))); cursor = s.cursor; }
      for (const e of s.events ?? []) {
        if (e.seq <= cursor) continue;
        cursor = e.seq;
        if (e.action) setActions(prev => new Map(prev).set(e.action!.id, e.action!));
        if (e.kind === "delta" && e.message_id) { const m = byId.get(e.message_id); if (m) byId.set(m.id, { ...m, text: m.text + (e.text ?? ""), html: "" }); }
        if (e.message) byId.set(e.message.id, e.message);
      }
      if (s.cursor > cursor) cursor = s.cursor;
      setActive(!!s.active);
      flush();
    };
    (async () => {
      while (!control.signal.aborted) {
        try { receive(await chat.wait(id, cursor, control.signal)); setProblem(null); }
        catch { if (control.signal.aborted) return; setProblem("Reconnecting…"); await new Promise(r => setTimeout(r, 1000)); }
      }
    })();
    return () => control.abort();
  }, [id]);
  useEffect(() => { log.current?.scrollTo({ top: log.current.scrollHeight }); }, [messages]);

  const send = async (text: string) => {
    const trimmed = text.trim();
    if (!trimmed) return;
    setDraft("");
    try {
      let conversation = id;
      if (!conversation) { conversation = (await chat.create()).id; setId(conversation); }
      await chat.send(conversation, trimmed, { page: "/app/" });
    } catch (e) { setProblem((e as Error).message); }
  };

  return <aside className="chat" aria-label="Chat">
    <header className="chat-head"><h2>Chat</h2><span className="muted">through your Claude Code login</span>{onClose && <button type="button" aria-label="Close chat" onClick={onClose}>×</button>}</header>
    <div className="chat-log" ref={log}>
      {messages.length === 0 && <Brief inbox={inbox} act={act} ask={text => void send(text)} />}
      {messages.map(m => {
        if (m.role === "panel") return m.text ? <PanelCard key={m.id} text={m.text} pinLabel={pinLabel} onPin={onPin} onSave={onSave} /> : null;
        if (m.role === "tool") return <p key={m.id} className="chat-tool">✓ {m.text.replace(/^mcp__driga__/, "")}</p>;
        if (m.role === "action") { const a = actions.get(m.id); return a && id ? <ActionCard key={m.id} conversation={id} action={a} /> : null; }
        if (m.role === "user") return <div key={m.id} className="chat-user">{m.text}</div>;
        return <div key={m.id} className="chat-assistant">{m.html ? <div dangerouslySetInnerHTML={{ __html: m.html }} /> : <p>{m.text}</p>}</div>;
      })}
      {active && <p className="muted">Working…</p>}
      {problem && <p className="problem">{problem}</p>}
    </div>
    <form className="chat-compose" onSubmit={e => { e.preventDefault(); void send(draft); }}>
      <label className="sr-only" htmlFor="chat-input">Ask or act</label>
      <textarea id="chat-input" ref={input} rows={2} value={draft} placeholder="Ask about your agents, or tell Driga what to show" onChange={e => setDraft(e.target.value)}
        onKeyDown={e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void send(draft); } }} />
      <div className="compose-row">
        <span className="muted">Actions wait for your approval</span>
        {active ? <button type="button" onClick={() => id && void chat.stop(id)}>Stop</button> : <button type="submit" className="primary-button">Send</button>}
      </div>
    </form>
  </aside>;
}
