// Search finds a session, live or ended in the last two weeks, and the dashboards, from ⌘K or the
// header's search button. Mantine's Spotlight filters by title, description
// and keywords; this file only supplies the actions.
import "@mantine/spotlight/styles.css";
import { Group, Kbd, Text, UnstyledButton, VisuallyHidden } from "@mantine/core";
import { isActionsGroup, Spotlight, spotlight, type SpotlightActionData, type SpotlightActionGroupData, type SpotlightFilterFunction } from "@mantine/spotlight";
import { ArrowSquareOut, ChatCircle, Copy, MagnifyingGlass, SquaresFour, Stack as StackIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { duration } from "../panels/format";
import type { Act } from "../panels/host";
import { rows, type Dashboard, type Fragment, type Row } from "../panels/types";
import { StatusBadge, stateOf } from "../widgets/StatusBadge";
import { dashboards, type Summary } from "./api";

// Live sessions come from sessions; ended ones from history, two weeks back.
const spec: Dashboard = { name: "Search", panels: [
  { id: "live", title: "Live", viz: "table", source: "sessions" },
  { id: "ended", title: "Ended", viz: "table", source: "history" },
] };

const ago = (now: number, at: unknown) => { const t = Date.parse(String(at ?? "")); return t ? `${duration(Math.max(0, now - t))} ago` : ""; };

/** sessionActions turns sessions into Spotlight actions. A live session
 * opens its pane. An ended one copies its resume command, or asks Chat about
 * it when its agent has none. Live sessions come first, newest first. */
export function sessionActions(live: Row[], ended: Row[], now: number, act: Act, resume: (r: Row) => void): SpotlightActionGroupData[] {
  const at = (r: Row) => Date.parse(String(r.last_activity_at ?? "")) || 0;
  const title = (r: Row) => String(r.title || `Session ${r.id}`);
  const keywords = (r: Row) => [r.project, r.branch, r.agent, r.machine, r.state].filter((k): k is string => typeof k === "string" && k !== "");
  const groups: SpotlightActionGroupData[] = [
    { group: "Live sessions", actions: [...live].sort((a, b) => at(b) - at(a)).map(r => ({
      id: `live-${r.machine_id}-${r.id}`, label: title(r),
      description: [r.project, r.branch, r.agent, r.machine, at(r) ? `active ${ago(now, r.last_activity_at)}` : ""].filter(Boolean).join(" · "),
      keywords: keywords(r),
      leftSection: <ArrowSquareOut size={16} aria-hidden />,
      rightSection: r.label ? <StatusBadge {...stateOf(String(r.label))} inferred={r.inferred === true || String(r.label).startsWith("~")} /> : undefined,
      onClick: () => void act({ kind: "jump", row: { ...r, session_id: r.id } }),
    })) },
    { group: "Ended sessions", actions: ended.map(r => ({
      id: `ended-${r.machine_id}-${r.id}`, label: title(r),
      description: [r.project, r.branch, r.agent, r.machine, `ended ${ago(now, r.ended_at)}`].filter(Boolean).join(" · "),
      keywords: keywords(r),
      leftSection: r.resume ? <Copy size={16} aria-hidden /> : <ChatCircle size={16} aria-hidden />,
      rightSection: <Text className="search-hint" size="xs" c="var(--driga-text)" style={{ whiteSpace: "nowrap" }}>{r.resume ? "Copy resume command" : "Ask Chat"}</Text>,
      onClick: () => resume(r),
    })) },
  ];
  return groups.filter(g => g.actions.length > 0);
}

/** filter keeps the groups in order, live sessions first, and matches every
 * word of the query against a session's title, description and keywords. */
export const filter: SpotlightFilterFunction = (query, actions) => {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return actions;
  const hit = (a: SpotlightActionData) => {
    const text = [a.label, a.description, ...(Array.isArray(a.keywords) ? a.keywords : [a.keywords])].filter(Boolean).join(" ").toLowerCase();
    return words.every(w => text.includes(w));
  };
  const out: (SpotlightActionData | SpotlightActionGroupData)[] = [];
  for (const a of actions) {
    if (!isActionsGroup(a)) { if (hit(a)) out.push(a); continue; }
    const kept = a.actions.filter(hit);
    if (kept.length) out.push({ ...a, actions: kept });
  }
  return out;
};

export function Search({ list, act, ask, open, onFleet, say }: { list: Summary[]; act: Act; ask: (text: string) => void; open: (id: string) => void;
  /** onFleet opens the Fleet page, listed beside the dashboards. */
  onFleet?: () => void; say: (message: string) => void }) {
  const [found, setFound] = useState<{ live: Row[]; ended: Row[] }>({ live: [], ended: [] });
  const [failed, setFailed] = useState(false);
  const of = (f: Fragment, id: string) => rows(f.results.find(r => r.id === id)?.frame ?? { columns: [], rows: [] });
  const load = () => dashboards.run(spec).then(f => { setFound({ live: of(f, "live"), ended: of(f, "ended") }); setFailed(false); }, () => setFailed(true));
  const resume = (r: Row) => {
    const title = String(r.title || `Session ${r.id}`);
    if (typeof r.resume !== "string" || !r.resume) { ask(`Tell me about session ${r.id}, "${title}": what it did and what it left uncommitted.`); return; }
    navigator.clipboard?.writeText(r.resume).then(() => say(`Copied the command that resumes ${title}. Paste it in a terminal.`), () => say(`Couldn't copy. Resume with: ${r.resume}`));
  };
  const boards: SpotlightActionGroupData = { group: "Dashboards", actions: list.map(d => ({ id: `dashboard-${d.id}`, label: d.name, leftSection: <SquaresFour size={16} aria-hidden />, onClick: () => open(d.id) })) };
  const pages: SpotlightActionGroupData = { group: "Pages", actions: onFleet ? [{ id: "page-fleet", label: "Fleet", description: "Every machine, project and session, and where the memory goes", keywords: ["machines", "sessions"], leftSection: <StackIcon size={16} aria-hidden />, onClick: onFleet }] : [] };
  return <>
    <UnstyledButton className="search-button" onClick={spotlight.open} aria-keyshortcuts="Meta+K" title="Search sessions and dashboards (⌘K)">
      <Group gap={8} wrap="nowrap">
        <MagnifyingGlass size={16} aria-hidden />
        <Text size="sm" c="dimmed" visibleFrom="lg">Search</Text>
        <VisuallyHidden hiddenFrom="lg">Search</VisuallyHidden>
        <Kbd size="xs" visibleFrom="lg" aria-hidden>⌘K</Kbd>
      </Group>
    </UnstyledButton>
    <Spotlight title="Search sessions and dashboards" styles={{ actionDescription: { color: "var(--driga-text)", opacity: 1 }, actionSection: { opacity: 1 } }} shortcut={["mod + K"]} filter={filter} onSpotlightOpen={() => void load()} limit={40} highlightQuery scrollable maxHeight={480}
      actions={[...sessionActions(found.live, found.ended, Date.now(), act, resume), ...(boards.actions.length ? [boards] : []), ...(pages.actions.length ? [pages] : [])]}
      nothingFound={failed ? "Driga can't reach its service, so there is nothing to search." : "No session or dashboard matches. Sessions that ended more than two weeks ago aren't listed."}
      searchProps={{ "aria-label": "Search sessions and dashboards", leftSection: <MagnifyingGlass size={18} aria-hidden />, placeholder: "Search sessions by title, project, branch, agent or machine" }} />
  </>;
}
