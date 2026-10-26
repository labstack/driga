// The app is a dashboard that fills the window with Chat docked beside it,
// under the header that says whether anything needs you. The Fleet is the
// other page. Every action goes through the app's own checks and dialogs.
import "@mantine/notifications/styles.css";
import { AppShell, Box, Button, Group, Select, Text, Title } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { spotlight } from "@mantine/spotlight";
import { PencilSimple } from "@phosphor-icons/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Panel as Pane, Group as Split, Separator, useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { FleetPage } from "../fleet/FleetPage";
import { fleetOf, fleetSpec } from "../fleet/FleetSpec";
import { HostContext, type Act, type Host, type InboxFilter } from "../panels/host";
import type { Dashboard as Spec, Fragment, Prompt } from "../panels/types";
import "../panels/panels.css";
import "./host.css";
import { action, chat, confirm, dashboards, type Operation, type Summary } from "./api";
import { Chat } from "./Chat";
import { ClustersHeader } from "./ClustersHeader";
import { Dashboard } from "./Dashboard";
import { attentionKinds, headerOf, kinds, type AttentionKind, type HeaderTarget } from "./Header";
import { historyOf } from "./History";
import { Search } from "./Search";

const refreshMs = 5000;
const headerID = "header";

// The brief reads the whole inbox, whatever dashboard is open.
const inboxSpec: Spec = { name: "Inbox", panels: [{ id: "inbox", title: "Inbox", viz: "inbox", source: "inbox" }] };

const homeID = "home";

type Page = "board" | "fleet";

function initial(): string {
  return new URLSearchParams(location.search).get("dashboard") || homeID;
}

function initialPage(): Page {
  return new URLSearchParams(location.search).get("page") === "fleet" ? "fleet" : "board";
}

// The Home panel each header target points at.
const targets: Record<HeaderTarget, string> = { inbox: "inbox", stuck: "stuck", leftover: "orphans", reclaim: "orphans", memory: "machine_memory" };

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

/** typing says whether a key went to a field, where it is text, not a command. */
function typing(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && (target.isContentEditable || /^(input|textarea|select)$/i.test(target.tagName));
}

export function App() {
  const [id, setId] = useState<string>(initial);
  const [page, setPage] = useState<Page>(initialPage);
  const [list, setList] = useState<Summary[]>([]);
  const [header, setHeader] = useState<Fragment | null>(null);
  const [inbox, setInbox] = useState<Fragment | null>(null);
  const [fleet, setFleet] = useState<Fragment | null>(null);
  const [fragment, setFragment] = useState<Fragment | null>(null);
  const [vars, setVars] = useState<Record<string, string>>({});
  const [editing, setEditing] = useState(false);
  const [filter, setFilter] = useState<InboxFilter | null>(null);
  const [chatOpen, setChatOpen] = useState(() => remembered("driga-chat-open") !== "false");
  // When the board last refreshed, and whether the latest try failed.
  const [fresh, setFresh] = useState<{ at: number; failed: boolean }>({ at: 0, failed: false });
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [ask, setAsk] = useState<{ text: string; at: number } | null>(null);
  const timer = useRef<number | undefined>(undefined);

  const say = (message: string) => notifications.show({ message, autoClose: 4000, closeButtonProps: { "aria-label": "Dismiss" } });
  const load = useCallback(async () => {
    const [h, i, f] = await Promise.allSettled([dashboards.open(headerID), dashboards.run(inboxSpec), dashboards.run(fleetSpec)]);
    if (h.status === "fulfilled") setHeader(h.value);
    if (i.status === "fulfilled") setInbox(i.value);
    if (f.status === "fulfilled") setFleet(f.value);
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
  // Chat and the board share the window; dragging the line between them
  // sets Chat's width, which is remembered. Hiding Chat collapses its pane.
  const chatPane = usePanelRef();
  const split = useDefaultLayout({ id: "driga-split", panelIds: ["board", "chat"], onlySaveAfterUserInteractions: true });
  useEffect(() => {
    const pane = chatPane.current;
    if (!pane) return;
    if (chatOpen && pane.isCollapsed()) pane.expand();
    if (!chatOpen && !pane.isCollapsed()) pane.collapse();
  }, [chatOpen, chatPane]);
  useEffect(() => { history.replaceState(null, "", page === "fleet" ? "/app/?page=fleet" : id === homeID ? "/app/" : `/app/?dashboard=${encodeURIComponent(id)}`); }, [id, page]);

  // The header reads the fragment's counts, the fleet, Chat's prompts and
  // the service’s persisted history.
  const fleetData = useMemo(() => fleet ? fleetOf(fleet, inbox) : null, [fleet, inbox]);
  const kept = useMemo(() => historyOf(header), [header]);
  const headerData = useMemo(() => header ? headerOf(header, fleetData, prompts.length, kept) : null, [header, fleetData, prompts.length, kept]);

  const open = (next: string) => { setEditing(false); setVars({}); setId(next); setPage("board"); };
  const openFleet = useCallback(() => { setEditing(false); setPage("fleet"); }, []);
  const showOnHome = (target: HeaderTarget) => {
    const away = id !== homeID || page !== "board";
    if (away) open(homeID);
    window.setTimeout(() => showPanel(targets[target]), away ? 600 : 0);
  };
  // A tile narrows the inbox to its kind, or points Home at the stuck sessions, which the inbox does not list.
  const onFilter = (kind: AttentionKind) => { if (kind === "stuck") showOnHome("stuck"); else setFilter(f => f === kind ? null : kind); };
  const askChat = (text: string) => { setChatOpen(true); setAsk({ text, at: Date.now() }); };
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
  const remove = async () => {
    if (!fragment?.id) return;
    const name = fragment.dashboard.name;
    if (!await confirm(`Delete the ${name} dashboard and its history?`)) { say("Cancelled"); return; }
    try { await dashboards.delete(fragment.id); if (fragment.id === homeID) void load(); else open(homeID); say(fragment.id === homeID ? "Home is back to the default" : `Deleted ${name}`); } catch (e) { say((e as Error).message); }
  };
  const host: Host = { act, local: (fragment ?? header)?.local_machine, prompts, answer, inboxFilter: filter, ask: askChat };

  // The app's menus drive the page through drigaCommand.
  useEffect(() => {
    (window as unknown as { drigaCommand: (name: string, arg?: unknown) => void }).drigaCommand = (name, arg) => {
      if (name === "reload") void load();
      else if (name === "back") history.back();
      else if (name === "forward") history.forward();
      else if (name === "find") spotlight.open();
      else if (name === "view" && arg === "fleet") openFleet();
      else if (name === "view" && arg === "inbox") { setFilter(null); showOnHome("inbox"); }
      // A session picked from the tray opens its pane, as clicking its row does.
      else if (name === "openSession" && arg && typeof arg === "object" && "session_id" in arg && typeof arg.session_id === "number") void act({ kind: "jump", row: arg as { session_id: number; machine_id?: string } });
    };
  });

  // The keys beside ⌘K: 1, 2 and 3 focus the attention tiles, g then f
  // opens the Fleet. Not while typing, and not under Search or any other
  // modal, which own the keys while they are open.
  useEffect(() => {
    let g = -Infinity;
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target) || document.querySelector('[aria-modal="true"]')) return;
      if (e.key === "g") { g = performance.now(); return; }
      if (e.key === "f" && performance.now() - g < 800) { g = -Infinity; openFleet(); return; }
      const kind = attentionKinds[Number(e.key) - 1];
      if (kind) document.querySelector<HTMLElement>(`[aria-keyshortcuts="${kinds[kind].key}"]`)?.focus();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [openFleet]);

  // The brief lists the inbox only when the dashboard does not.
  const inboxPanel = inbox?.dashboard.panels[0];
  // Until the open dashboard arrives, whether it lists the inbox is unknown;
  // the brief then shows no copy of it rather than one that vanishes.
  const listed = page === "fleet" ? false : fragment ? fragment.dashboard.panels.some(p => p.viz === "inbox") : true;
  const brief = inboxPanel ? { panel: inboxPanel, result: inbox!.results[0], listed: !!listed } : undefined;
  const d = fragment?.dashboard;
  return <HostContext.Provider value={host}>
    <AppShell header={{ height: 56 }} padding={0}>
      <AppShell.Header px="md" style={{ background: "color-mix(in srgb, var(--driga-ground) 88%, transparent)", backdropFilter: "blur(10px)", borderColor: "var(--driga-border)" }}>
        <ClustersHeader data={headerData} filter={filter} chatOpen={chatOpen} onFilter={onFilter} onOpenFleet={openFleet} onSearch={spotlight.open} onToggleChat={() => setChatOpen(o => !o)}
          onShow={showOnHome} onAsk={about => askChat(`Tell me about "${about}" right now.`)}
          search={<Search list={list} act={act} say={say} open={open} onFleet={openFleet} ask={askChat} />} />
      </AppShell.Header>
      <AppShell.Main style={{ height: "100dvh", display: "flex" }}>
       <Split orientation="horizontal" id="driga-split" defaultLayout={split.defaultLayout} onLayoutChanged={split.onLayoutChanged} style={{ flex: 1, minWidth: 0 }}>
       <Pane id="board" minSize={360} style={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
        {page === "fleet"
          ? <Box px="md" pt="sm" pb="md" style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
            <FleetPage fragment={fleet} inbox={inbox} act={act} />
          </Box>
          : <>
        <Group justify="space-between" wrap="nowrap" gap="sm" px="md" pt="sm" pb={8}>
          <Box style={{ minWidth: 0 }}>
            <Title order={1} fz={28} fw={650} lh={1.2} style={{ letterSpacing: "-0.02em" }}>{d?.name ?? "Loading…"}</Title>
            {fresh.failed && fresh.at > 0
              ? <Text size="xs" c="warning" truncate role="status" title={`Not updated since ${new Date(fresh.at).toLocaleTimeString([], { timeStyle: "medium" })}. Retrying; check driga service status.`}>Not updated since {new Date(fresh.at).toLocaleTimeString([], { timeStyle: "medium" })}. Retrying; check <Text span size="xs" ff="monospace">driga service status</Text>.</Text>
              : fresh.failed ? <Text size="xs" c="warning" role="status">Driga can't reach its service. Retrying every {refreshMs / 1000}s.</Text>
              : d?.description && <Text size="xs" c="dimmed" truncate title={d.description}>{d.description}</Text>}
          </Box>
          <Group gap="xs" wrap="nowrap">
            {d?.variables?.map(v => <Select key={v.name} size="xs" w={140} aria-label={v.name} placeholder={`All ${v.name}s`} clearable
              data={fragment?.options?.[v.name] ?? []} value={vars[v.name] || null} onChange={next => setVars(prev => ({ ...prev, [v.name]: next ?? "" }))} />)}
            {list.length > 1 && <Select size="xs" w={170} aria-label="Dashboard" allowDeselect={false} data={list.map(x => ({ value: x.id, label: x.name }))} value={id} onChange={next => next && open(next)} />}
            {editing && <Button variant="light" color="danger" onClick={() => void remove()}>{id === homeID ? "Reset Home" : "Delete dashboard"}</Button>}
            <Button variant={editing ? "filled" : "default"} leftSection={<PencilSimple />} aria-pressed={editing} onClick={() => setEditing(e => !e)}>{editing ? "Done" : "Edit"}</Button>
          </Group>
        </Group>
        {editing && <Text size="xs" c="dimmed" px="md" pb={6}>Drag panels by their title, resize from the corner, or ask Chat to change any of it.</Text>}
        <Box px="md" pb="md" style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
          {fragment ? <Dashboard fragment={fragment} editing={editing} act={act}
            onLayout={grids => { if (fragment) void edit(fragment.id!, fragment.version ?? 0, [{ op: "layout", grids }], "Moved panels"); }}
            onRemove={pid => { if (fragment) void edit(fragment.id!, fragment.version ?? 0, [{ op: "remove", id: pid }], "Removed a panel"); }} /> : <Text size="sm" c="dimmed">Loading…</Text>}
        </Box>
          </>}
       </Pane>
       <Separator className="split-handle" aria-label="Resize Chat" />
       <Pane id="chat" panelRef={chatPane} defaultSize={400} minSize={300} maxSize="65" collapsible collapsedSize={0}
         onResize={size => { const shown = size.inPixels > 0; if (shown !== chatOpen) setChatOpen(shown); }}
         style={{ background: "var(--driga-inset)", borderLeft: "1px solid var(--driga-border)", minWidth: 0 }}>
        {/* Chat stays mounted while collapsed, so a draft and the conversation survive. */}
        <Chat inbox={brief} act={act} ask={ask} onOpen={open} onClose={() => setChatOpen(false)} />
       </Pane>
       </Split>
      </AppShell.Main>
    </AppShell>
  </HostContext.Provider>;
}
