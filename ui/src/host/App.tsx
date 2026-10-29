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
import { fleetOf } from "../fleet/FleetSpec";
import { HostContext, type Act, type Host, type InboxFilter } from "../panels/host";
import { rows, type Dashboard as DashboardSpec, type Panel, type Prompt } from "../panels/types";
import "../panels/panels.css";
import "./host.css";
import { action, confirmedAction, chat, confirm, dashboards, type Operation } from "./api";
import { Chat } from "./Chat";
import { ClustersHeader } from "./ClustersHeader";
import { Dashboard } from "./Dashboard";
import { attentionKinds, headerOf, type AttentionKind, type HeaderTarget } from "./Header";
import { HeaderEditor } from "./HeaderEditor";
import { historyOf } from "./History";
import { Search } from "./Search";
import { useDashboardRefresh, refreshMs, headerID, homeID } from "./useDashboardRefresh";

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
  const [vars, setVars] = useState<Record<string, string>>({});
  const [shown, setShown] = useState<string | null>(null);
  const [filter, setFilter] = useState<InboxFilter | null>(null);
  const [chatOpen, setChatOpen] = useState(() => remembered("driga-chat-open") !== "false");
  const [ask, setAsk] = useState<{ text: string; at: number } | null>(null);
  const say = (message: string) => notifications.show({ message, autoClose: 4000, closeButtonProps: { "aria-label": "Dismiss" } });
  const { fragment, header, inbox, fleet, prompts, list, fresh, editing, setEditing, refresh: load, edit, mutateHeader, forgetPrompt } = useDashboardRefresh({ id, vars, onError: say, onMissing: () => setId(homeID) });
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

  // The header reads the server's reconciled counts, the fleet and history.
  const fleetData = useMemo(() => fleet ? fleetOf(fleet, inbox) : null, [fleet, inbox]);
  const kept = useMemo(() => historyOf(header), [header]);
  const headerData = useMemo(() => header ? headerOf(header, fleetData, kept) : null, [header, fleetData, kept]);

  useEffect(() => {
    if (!shown || id !== homeID || page !== "board" || fragment?.id !== homeID) return;
    const reveal = () => {
      if (!document.getElementById(`panel-${shown}`)) return false;
      showPanel(shown);
      setShown(null);
      return true;
    };
    if (reveal()) return;
    const observer = new MutationObserver(() => { if (reveal()) observer.disconnect(); });
    observer.observe(document.body, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, [shown, id, page, fragment]);

  const open = (next: string) => { setEditing(false); setVars({}); setId(next); setPage("board"); };
  const openFleet = useCallback(() => { setEditing(false); setPage("fleet"); }, []);
  const showTarget = (target: HeaderTarget) => {
    const away = id !== homeID || page !== "board";
    if (away) open(homeID);
    setShown(targets[target]);
  };
  const showOnHome = async (panel: Panel) => {
    try {
      const home = fragment?.id === homeID ? fragment : await dashboards.open(homeID);
      let match = home.dashboard.panels.find(p => p.source === panel.source && (p.measure ?? "count") === (panel.measure ?? "count"));
      if (!match) {
        let id = panel.id;
        for (let n = 2; home.dashboard.panels.some(p => p.id === id); n++) id = `${panel.id}_${n}`;
        const values = { ...Object.fromEntries((header?.dashboard.variables ?? []).map(v => [v.name, v.default ?? ""])), ...header?.vars };
        let title = panel.title;
        // Match the engine's substitution order for names sharing a prefix.
        for (const name of Object.keys(values).sort((a, b) => b.length - a.length)) title = title.replaceAll(`$${name}`, values[name] || `all ${name}s`);
        match = { ...panel, id, title, ...(panel.where ? { where: panel.where.flatMap(f => {
          if (!f.value.startsWith("$")) return [f];
          const value = values[f.value.slice(1)];
          // An empty variable selects all values; a literal empty filter does not.
          return value ? [{ ...f, value }] : [];
        }) } : {}) };
        const next = await edit(homeID, home.version ?? 0, [{ op: "add", panel: match }], "Added a header widget to Home");
        if (!next) return;
      }
      const away = id !== homeID || page !== "board";
      if (away) open(homeID);
      setShown(match.id);
    } catch (e) { say((e as Error).message); }
  };
  const onFilter = (kind: AttentionKind) => setFilter(f => f === kind ? null : kind);
  const askChat = (text: string) => { setChatOpen(true); setAsk({ text, at: Date.now() }); };
  const [headerBusy, setHeaderBusy] = useState(false);
  const savingHeader = useRef(false);
  const headerChange = async (save: () => ReturnType<typeof dashboards.open>, previous: DashboardSpec, message: string) => {
    if (savingHeader.current) return;
    savingHeader.current = true;
    setHeaderBusy(true);
    try {
      const next = await mutateHeader(save);
      if (!next) return;
      const toast = notifications.show({ autoClose: 8000, closeButtonProps: { "aria-label": "Dismiss" }, message: <Group gap="sm" wrap="nowrap">{message}<Button size="compact-xs" variant="subtle" onClick={() => {
        if (savingHeader.current) return;
        notifications.hide(toast);
        void headerChange(() => dashboards.replace(headerID, previous, next.version ?? 0, undefined, "Undid a header edit"), structuredClone(next.dashboard), "Header restored");
      }}>Undo</Button></Group> });
    } finally { savingHeader.current = false; setHeaderBusy(false); }
  };
  const editHeader = (operations: Operation[], message: string) => {
    if (header) void headerChange(() => dashboards.edit(headerID, operations, header.version ?? 0, undefined, message), structuredClone(header.dashboard), message);
  };
  const resetHeader = () => {
    if (header) void headerChange(() => dashboards.reset(headerID, header.version ?? 0), structuredClone(header.dashboard), "Header reset");
  };
  const act: Act = async ({ kind, row }) => {
    const machine = row.machine_id;
    const session = Number(row.session_id ?? row.id);
    const name = String(row.title ?? row.signature ?? row.project ?? row.path ?? row.id ?? "");
    // Anything that changes the machine asks the app's dialog first, as the
    // TUI does; the server refuses it otherwise.
    const ask = async (message: string, body: Record<string, unknown>) => { const ok = await confirmedAction(message, body); if (!ok) say("Cancelled"); return ok; };
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
          if (!await ask(`Stop ${name}? Its agent and the processes it started are ended.`, { action: "kill", session_id: session, machine })) return; say(`Stopped ${name}`); break;
        case "kill":
        case "force_kill": {
          const force = kind === "force_kill";
          if (!await ask(`${force ? "Force kill" : "Kill"} the ${name} process tree? ${force ? "Processes get SIGKILL without a chance to clean up." : "Processes get SIGTERM first."}`, { action: "kill", id: row.id, machine, force })) return; say(`Killed ${name}`); break;
        }
        case "cleanup":
          if (!await ask(`Remove ${name}? Driga checks it again before removing it.`, { action: "cleanup", id: row.id, machine })) return; say(`Removed ${name}`); break;
        case "summarize": await action({ action: "summarize", project: row.path, machine }); say(`Summarizing ${name}`); return;
      }
      void load();
    } catch (e) { say((e as Error).message); }
  };
  const answer = async (prompt: Prompt, allow: boolean) => {
    try { await chat.answer(prompt.id, allow); forgetPrompt(prompt.id); say(allow ? `Allowed ${prompt.tool}` : `Denied ${prompt.tool}`); }
    catch (e) { say((e as Error).message); }
  };
  const remove = async () => {
    if (!fragment?.id) return;
    const name = fragment.dashboard.name;
    if (!await confirm(`Delete the ${name} dashboard and its history?`)) { say("Cancelled"); return; }
    try { await dashboards.delete(fragment.id); if (fragment.id === homeID) void load(); else open(homeID); say(fragment.id === homeID ? "Home is back to the default" : `Deleted ${name}`); } catch (e) { say((e as Error).message); }
  };
  const sessions = fleet?.results.find(r => r.id === "sessions")?.frame;
  const sessionRows = rows(sessions);
  const stuck = sessions ? { ...sessions, rows: sessions.rows.filter((_, i) => sessionRows[i].state === "stuck" || sessionRows[i].label === "stuck") } : undefined;
  const host: Host = { clearInboxFilter: () => setFilter(null), stuck, act, local: (fragment ?? header)?.local_machine, prompts, answer, inboxFilter: filter, ask: askChat };

  // The app's menus drive the page through drigaCommand.
  useEffect(() => {
    (window as unknown as { drigaCommand: (name: string, arg?: unknown) => void }).drigaCommand = (name, arg) => {
      if (name === "reload") void load();
      else if (name === "back") history.back();
      else if (name === "forward") history.forward();
      else if (name === "find") spotlight.open();
      else if (name === "view" && arg === "fleet") openFleet();
      else if (name === "view" && arg === "inbox") { setFilter(null); showTarget("inbox"); }
      // A session picked from the tray opens its pane, as clicking its row does.
      else if (name === "openSession" && arg && typeof arg === "object" && "session_id" in arg && typeof arg.session_id === "number") void act({ kind: "jump", row: arg as { session_id: number; machine_id?: string } });
    };
  });

  // The keys beside ⌘K: 1, 2 and 3 filter the inbox, g then f
  // opens the Fleet. Not while typing, and not under Search or any other
  // modal, which own the keys while they are open.
  useEffect(() => {
    let g = -Infinity;
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target) || document.querySelector('[aria-modal="true"]')) { g = -Infinity; return; }
      const followsG = performance.now() - g < 800;
      g = -Infinity;
      if (e.key === "g") { g = performance.now(); return; }
      if (e.key === "f" && followsG) { openFleet(); return; }
      const kind = attentionKinds[Number(e.key) - 1];
      if (kind) { e.preventDefault(); setFilter(f => f === kind ? null : kind); }
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
          onShow={panel => void showOnHome(panel)} busy={headerBusy} onRemove={pid => editHeader([{ op: "remove", id: pid }], "Removed a header widget")}
          onMove={(pid, before) => editHeader([{ op: "move", id: pid, ...(before ? { before } : {}) }], "Reordered header widgets")}
          editor={header && <HeaderEditor panels={header.dashboard.panels} busy={headerBusy} onAdd={panel => editHeader([{ op: "add", panel }], "Added a header widget")} onReset={resetHeader} onAsk={askChat} />}
          onAsk={about => askChat(`Tell me about "${about}" right now.`)}
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
       <Pane id="chat" inert={!chatOpen} panelRef={chatPane} defaultSize={400} minSize={300} maxSize="65" collapsible collapsedSize={0}
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
