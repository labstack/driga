// The app is a dashboard that fills the window with Chat docked beside it,
// under the few numbers that say whether anything needs you. Every action
// goes through the app's own checks and dialogs.
import "@mantine/notifications/styles.css";
import { ActionIcon, AppShell, Box, Button, Group, Select, Text, Title, Tooltip } from "@mantine/core";
import { Notifications, notifications } from "@mantine/notifications";
import { spotlight } from "@mantine/spotlight";
import { ChatCircle, PencilSimple } from "@phosphor-icons/react";
import { StrictMode, useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { HostContext, type Act, type Host } from "../panels/host";
import type { Dashboard as Spec, Fragment, Panel, Prompt } from "../panels/types";
import { Provider } from "../theme/Provider";
import "../panels/panels.css";
import "./host.css";
import { action, chat, confirm, dashboards, type Operation, type Summary } from "./api";
import { Brand } from "./Brand";
import { Chat } from "./Chat";
import { Dashboard } from "./Dashboard";
import { Metrics } from "./Metrics";
import { Search } from "./Search";

const refreshMs = 5000;
const headerID = "header";

// The brief reads the whole inbox, whatever dashboard is open.
const inboxSpec: Spec = { name: "Inbox", panels: [{ id: "inbox", title: "Inbox", viz: "inbox", source: "inbox" }] };

const homeID = "home";

function initial(): string {
  return new URLSearchParams(location.search).get("dashboard") || homeID;
}

/** showPanel scrolls a panel into view and marks it for a moment. */
function showPanel(id: string) {
  const title = document.getElementById(`panel-${id}`);
  const el = title?.closest(".mantine-Card-root") ?? title;
  if (!el) return;
  el.scrollIntoView({ behavior: "smooth", block: "nearest" });
  el.classList.add("panel-shown");
  window.setTimeout(() => el.classList.remove("panel-shown"), 1600);
}

// remembered and remember keep small per-viewer settings; storage can be
// missing or refuse, and the app works the same without it.
function remembered(key: string): string | null {
  try { return localStorage.getItem(key); } catch { return null; }
}
function remember(key: string, value: string) {
  try { localStorage.setItem(key, value); } catch { /* not kept */ }
}

function App() {
  const [id, setId] = useState<string>(initial);
  const [list, setList] = useState<Summary[]>([]);
  const [header, setHeader] = useState<Fragment | null>(null);
  const [inbox, setInbox] = useState<Fragment | null>(null);
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [vars, setVars] = useState<Record<string, string>>({});
  const [editing, setEditing] = useState(false);
  const [chatOpen, setChatOpen] = useState(() => remembered("driga-chat-open") !== "false");
  // When the board last refreshed, and whether the latest try failed.
  const [fresh, setFresh] = useState<{ at: number; failed: boolean }>({ at: 0, failed: false });
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [ask, setAsk] = useState<{ text: string; at: number } | null>(null);
  const timer = useRef<number | undefined>(undefined);

  const say = (message: string) => notifications.show({ message, autoClose: 4000 });
  const load = useCallback(async () => {
    const [h, i] = await Promise.allSettled([dashboards.open(headerID), dashboards.run(inboxSpec)]);
    if (h.status === "fulfilled") setHeader(h.value);
    if (i.status === "fulfilled") setInbox(i.value);
    // A failed fetch keeps the prompts last seen; the stale note says so.
    chat.prompts().then(setPrompts, () => undefined);
    try { setFragment(await dashboards.open(id, vars)); setFresh({ at: Date.now(), failed: false }); }
    catch (e) {
      setFresh(f => ({ ...f, failed: true }));
      if (id !== homeID) { say((e as Error).message); setId(homeID); }
    }
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
  useEffect(() => { remember("driga-chat-open", String(chatOpen)); }, [chatOpen]);
  useEffect(() => { history.replaceState(null, "", id === homeID ? "/app/" : `/app/?dashboard=${encodeURIComponent(id)}`); }, [id]);

  const open = (next: string) => { setEditing(false); setVars({}); setId(next); };
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
        case "ack": {
          // A session's finished turns are one row; acknowledging it clears every one.
          const ids = typeof row.ids === "string" && row.ids ? row.ids.split(",") : [row.id];
          for (const id of ids) await action({ action: "ack_inbox", id, machine });
          say(ids.length > 1 ? `Acknowledged ${ids.length} turns` : "Acknowledged");
          break;
        }
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
  // A chat card's panels join the open dashboard, or become a new one.
  const pin = async (panels: Panel[]) => {
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
    }
  };
  const save = async (panels: Panel[], name: string) => {
    try {
      const created = await dashboards.create({ name, panels: panels.map(p => ({ ...p, grid: undefined })) }, "Saved from Chat");
      if (created.id) open(created.id);
      say(`Saved ${name}`);
    } catch (e) { say((e as Error).message); }
  };
  const remove = async () => {
    if (!fragment?.id) return;
    const name = fragment.dashboard.name;
    if (!await confirm(`Delete the ${name} dashboard and its history?`)) { say("Cancelled"); return; }
    try { await dashboards.delete(fragment.id); if (fragment.id === homeID) void load(); else open(homeID); say(fragment.id === homeID ? "Home is back to the default" : `Deleted ${name}`); } catch (e) { say((e as Error).message); }
  };
  const host: Host = { act, local: (fragment ?? header)?.local_machine, prompts, answer };

  // The app's menus drive the page through drigaCommand.
  useEffect(() => {
    (window as unknown as { drigaCommand: (name: string, arg?: unknown) => void }).drigaCommand = (name, arg) => {
      if (name === "reload") void load();
      else if (name === "back") history.back();
      else if (name === "forward") history.forward();
      else if (name === "find") spotlight.open();
      // A session picked from the tray opens its pane, as clicking its row does.
      else if (name === "openSession" && typeof arg === "number") void act({ kind: "jump", row: { session_id: arg } });
    };
  });

  // The brief lists the inbox only when the dashboard does not.
  const inboxPanel = inbox?.dashboard.panels[0];
  const shown = fragment?.dashboard.panels.some(p => p.viz === "inbox");
  const brief = inboxPanel ? { panel: inboxPanel, result: inbox!.results[0], listed: !!shown } : undefined;
  const d = fragment?.dashboard;
  return <HostContext.Provider value={host}>
    <AppShell header={{ height: 56 }} aside={{ width: 400, breakpoint: "md", collapsed: { desktop: !chatOpen, mobile: !chatOpen } }} padding={0}>
      <AppShell.Header px="md" style={{ background: "color-mix(in srgb, var(--driga-ground) 88%, transparent)", backdropFilter: "blur(10px)", borderColor: "var(--driga-border)" }}>
        <Group h="100%" gap="md" wrap="nowrap">
          <Brand />
          <Metrics fragment={header} editing={editing} extra={{ waiting: prompts.length }} onAsk={m => { setChatOpen(true); setAsk({ text: `Tell me about "${m.panel.title}" right now.`, at: Date.now() }); }}
            onShow={target => { if (id !== homeID) open(homeID); window.setTimeout(() => showPanel(target), id !== homeID ? 600 : 0); }}
            onRemove={pid => header && void edit(headerID, header.version ?? 0, [{ op: "remove", id: pid }], "Removed a metric")} />
          <Search list={list} act={act} say={say} open={open} ask={text => { setChatOpen(true); setAsk({ text, at: Date.now() }); }} />
          <Tooltip label={chatOpen ? "Hide Chat" : "Show Chat"}>
            <ActionIcon size="lg" variant={chatOpen ? "light" : "subtle"} color={chatOpen ? "brand" : "gray"} aria-pressed={chatOpen} aria-label="Chat" onClick={() => setChatOpen(o => !o)}><ChatCircle size={20} /></ActionIcon>
          </Tooltip>
        </Group>
      </AppShell.Header>
      <AppShell.Main style={{ height: "100dvh", display: "flex", flexDirection: "column" }}>
        <Group justify="space-between" wrap="nowrap" gap="sm" px="md" pt="sm" pb={8}>
          <Box style={{ minWidth: 0 }}>
            <Title order={1} fz={28} fw={650} lh={1.2} style={{ letterSpacing: "-0.02em" }}>{d?.name ?? "Loading…"}</Title>
            {fresh.failed && fresh.at > 0
              ? <Text size="xs" c="warning" truncate role="status">Not updated since {new Date(fresh.at).toLocaleTimeString([], { timeStyle: "medium" })}. Retrying; check <Text span size="xs" ff="monospace">driga service status</Text>.</Text>
              : fresh.failed ? <Text size="xs" c="warning" role="status">Driga can't reach its service. Retrying every {refreshMs / 1000}s.</Text>
              : d?.description && <Text size="xs" c="dimmed" truncate>{d.description}</Text>}
          </Box>
          <Group gap="xs" wrap="nowrap">
            {d?.variables?.map(v => <Select key={v.name} size="xs" w={140} aria-label={v.name} placeholder={`All ${v.name}s`} clearable
              data={fragment?.options?.[v.name] ?? []} value={vars[v.name] || null} onChange={next => setVars(prev => ({ ...prev, [v.name]: next ?? "" }))} />)}
            {list.length > 1 && <Select size="xs" w={170} aria-label="Dashboard" allowDeselect={false} data={list.map(x => ({ value: x.id, label: x.name }))} value={id} onChange={next => next && open(next)} />}
            {editing && <Button variant="light" color="danger" onClick={() => void remove()}>{id === homeID ? "Reset Home" : "Delete dashboard"}</Button>}
            <Button variant={editing ? "filled" : "default"} leftSection={<PencilSimple />} aria-pressed={editing} onClick={() => setEditing(e => !e)}>{editing ? "Done" : "Edit"}</Button>
          </Group>
        </Group>
        {editing && <Text size="xs" c="dimmed" px="md" pb={6}>Drag panels by their title, resize from the corner, remove a header metric with its ×, or ask Chat to change any of it.</Text>}
        <Box px="md" pb="md" style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
          {fragment ? <Dashboard fragment={fragment} editing={editing} act={act}
            onLayout={grids => void edit(fragment.id!, fragment.version ?? 0, [{ op: "layout", grids }], "Moved panels")}
            onRemove={pid => void edit(fragment.id!, fragment.version ?? 0, [{ op: "remove", id: pid }], "Removed a panel")} /> : <Text size="sm" c="dimmed">Loading…</Text>}
        </Box>
      </AppShell.Main>
      <AppShell.Aside style={{ borderColor: "var(--driga-border)", background: "var(--driga-inset)" }}>
        {/* Chat stays mounted while hidden, so a draft and the conversation survive. */}
        <Box display={chatOpen ? "contents" : "none"}><Chat inbox={brief} act={act} ask={ask} pinLabel={`Add to ${d?.name ?? "dashboard"}`} onPin={pin} onSave={save} onClose={() => setChatOpen(false)} /></Box>
      </AppShell.Aside>
    </AppShell>
  </HostContext.Provider>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><Provider><Notifications position="bottom-center" /><App /></Provider></StrictMode>);
