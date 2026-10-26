import { ActionIcon, Box, Divider, Group, Kbd, Progress, Stack, Text, Tooltip, UnstyledButton, VisuallyHidden } from "@mantine/core";
import { Broom, ChatCircle, CheckCircle, MagnifyingGlass, Memory, SquaresFour, Stack as StackIcon, TreeStructure } from "@phosphor-icons/react";
import { bytes, exact } from "../panels/format";
import { pressureTone } from "../widgets/BarGauge";
import { KpiTile, type MenuItem } from "../widgets/KpiTile";
import { stateVar } from "../widgets/StatusBadge";
import { Brand } from "./Brand";
import { attentionKinds, changeWords, clock, fleetNotes, fleetSummary, kinds, machineSummary, percent, worst, type AttentionKind, type HeaderData, type HeaderProps, type Machine } from "./Header";
import classes from "./Header.module.css";

/** A hairline between clusters. */
function Rule() {
  return <Divider orientation="vertical" h={24} color="var(--driga-border)" style={{ alignSelf: "center" }} />;
}

/** A fleet dot: the machine's worst session state, hollow when
 * unreachable, ringed in the accent for this machine. */
function Dot({ m, now }: { m: Machine; now: number }) {
  const state = worst(m);
  const fill = !m.reachable ? "transparent" : state ? stateVar(state) : "var(--driga-faint)";
  return <Tooltip label={machineSummary(m, now)} events={{ hover: true, focus: false, touch: true }}>
    <Box w={8} h={8} aria-hidden style={{ borderRadius: "50%", background: fill, flex: "none", boxShadow: !m.reachable ? "inset 0 0 0 1.5px var(--driga-faint)" : undefined, outline: m.local ? "1px solid var(--mantine-color-brand-filled)" : undefined, outlineOffset: 1 }} />
  </Tooltip>;
}

const maxDots = 8;

/** The tile's menu: Show on Home and Ask Chat, as the header's tiles have always had. */
function tileMenu(label: string, onShow: () => void, onAsk?: (about: string) => void): MenuItem[] {
  return [{ label: "Show on Home", icon: SquaresFour, onClick: onShow }, ...(onAsk ? [{ label: "Ask Chat about it", icon: ChatCircle, onClick: () => onAsk(label) }] : [])];
}

/** ClustersHeader is the header's content row, inside the app shell: the
 * mark, then four clusters set apart by hairlines (attention, hygiene, this
 * machine and the fleet), then search and Chat. The clusters are KPI tiles
 * at their header size. A count that needs the person takes its tone;
 * zeros stay quiet; when nothing needs them the attention tiles fold into
 * one line. The keys 1, 2 and 3 and g then f are the app's. */
export function ClustersHeader({ data, filter, chatOpen, onFilter, onOpenFleet, onSearch, onToggleChat, onShow, onAsk, search }: HeaderProps) {
  const now = Date.now();
  // One tree whether the data is here or not, so search and the Chat toggle
  // stay the same elements when the first load fills the clusters in.
  return <Group h="100%" gap={8} wrap="nowrap">
    <Brand />
    {data && <Clusters data={data} filter={filter} now={now} onFilter={onFilter} onOpenFleet={onOpenFleet} onShow={onShow} onAsk={onAsk} />}
    <Box style={{ flex: 1 }} />
    {search ?? <UnstyledButton className="search-button" onClick={onSearch} aria-keyshortcuts="Meta+K" title="Search sessions and dashboards (⌘K)">
      <Group gap={8} wrap="nowrap">
        <MagnifyingGlass size={16} aria-hidden />
        <Text size="sm" c="dimmed" visibleFrom="lg">Search</Text>
        <VisuallyHidden hiddenFrom="lg">Search</VisuallyHidden>
        <Kbd size="xs" visibleFrom="lg" aria-hidden>⌘K</Kbd>
      </Group>
    </UnstyledButton>}
    <Tooltip label={chatOpen ? "Hide Chat" : "Show Chat"}>
      <ActionIcon size="lg" variant={chatOpen ? "light" : "subtle"} color={chatOpen ? "brand" : "gray"} aria-pressed={chatOpen} aria-label="Chat" onClick={onToggleChat}><ChatCircle size={20} /></ActionIcon>
    </Tooltip>
  </Group>;
}

/** Clusters is the header's middle: attention, hygiene, this machine and the fleet. */
function Clusters({ data, filter, now, onFilter, onOpenFleet, onShow, onAsk }: { data: HeaderData; filter?: AttentionKind | null; now: number } & Pick<HeaderProps, "onFilter" | "onOpenFleet" | "onShow" | "onAsk">) {
  const { attention, hygiene, memory, machines, checkedAt } = data;
  const clear = attentionKinds.every(k => attention[k].count === 0);
  const pct = percent(memory.used, memory.total);
  const leftover = hygiene.leftover;
  const notes = fleetNotes(machines);
  const fleet = fleetSummary(machines);
  const worstOverall = machines.map(worst).find(s => s === "stuck") ?? machines.map(worst).find(s => s === "waiting") ?? null;
  const fleetHint = <Group gap={6} wrap="nowrap">{fleet}<Kbd size="sm">g f</Kbd></Group>;
  const tooltip = (kind: AttentionKind) => <Group gap={6} wrap="nowrap">{kinds[kind].hint} · {changeWords(attention[kind])}<Kbd size="sm">{kinds[kind].key}</Kbd></Group>;
  const attentionTile = (kind: AttentionKind) => <KpiTile key={kind} size="header" label={kinds[kind].label} icon={kinds[kind].icon} value={attention[kind].count} unit="count" attention={kind}
    tooltip={tooltip(kind)} shortcut={kinds[kind].key} selected={filter === kind} onClick={() => onFilter(kind)} menu={tileMenu(kinds[kind].label, () => onShow(kinds[kind].target), onAsk)} />;
  const leftoverTile = (fold: boolean) => <KpiTile size="header" label={fold ? "Reclaim" : "Leftover"} icon={fold ? Broom : TreeStructure}
    value={leftover.count ?? leftover.bytes} unit={leftover.count === undefined ? "bytes" : "count"}
    caption={fold ? bytes(hygiene.reclaimable) : leftover.count !== undefined && leftover.bytes ? bytes(leftover.bytes) : undefined}
    detail={fold ? `leftover process trees, ${bytes(leftover.bytes)}; ${bytes(hygiene.reclaimable)} reclaimable` : leftover.count === undefined ? "of leftover process trees" : "process trees"}
    tooltip={fold ? `${leftover.count ?? "Some"} leftover process trees, ${bytes(leftover.bytes)}; a cleanup would free ${bytes(hygiene.reclaimable)}` : "Process trees their sessions left behind"}
    onClick={() => onShow(fold ? "reclaim" : "leftover")} menu={tileMenu(fold ? "Reclaim" : "Leftover", () => onShow(fold ? "reclaim" : "leftover"), onAsk)} visibleFrom={fold ? undefined : "lg"} hiddenFrom={fold ? "lg" : undefined} />;

  return <>
    <Rule />

    {clear
      ? <Group gap={8} wrap="nowrap" role="status" miw={240} px={8} py={4}>
        <Box w={20} h={20} c="success" style={{ borderRadius: "50%", background: "var(--driga-tint-success)", display: "grid", placeItems: "center", flex: "none" }}><CheckCircle size={14} aria-hidden /></Box>
        <Stack gap={0}>
          <Text fz={13} fw={500} lh={1.2} style={{ whiteSpace: "nowrap" }}>Nothing needs you</Text>
          <Text fz={12} ff="monospace" c="dimmed" lh={1.2} title={exact(checkedAt)} style={{ whiteSpace: "nowrap" }}>checked {clock(checkedAt)}</Text>
        </Stack>
      </Group>
      : <Group gap={8} wrap="nowrap" role="group" aria-label="Attention">{attentionKinds.map(attentionTile)}</Group>}
    <Rule />

    {leftoverTile(false)}
    <KpiTile size="header" label="Reclaimable" icon={Broom} value={hygiene.reclaimable} unit="bytes" tooltip="What a cleanup would free; opens the Reclaim panel" onClick={() => onShow("reclaim")} menu={tileMenu("Reclaimable", () => onShow("reclaim"), onAsk)} visibleFrom="lg" />
    {leftoverTile(true)}
    <Rule />

    <KpiTile size="header" label="Memory" sublabel={memory.machine} icon={Memory} value={memory.used} unit="bytes" total={memory.total} trend={memory.trend}
      delta={{ change: memory.delta, window: "1h" }} direction="down-good"
      tooltip={`${bytes(memory.used)} of ${bytes(memory.total)} on ${memory.machine}; opens the machine`} onClick={() => onShow("memory")} menu={tileMenu("Memory", () => onShow("memory"), onAsk)}>
      {/* The accent until the danger threshold, red from it: a dev machine sits at 70–80% all day. */}
      <Progress size="xs" value={pct} color={pressureTone(pct / 100, [0.9, 0.9])} aria-label={`${pct}% of memory used`} styles={{ root: { background: "var(--driga-border)" } }} />
    </KpiTile>
    <Rule />

    <Tooltip label={fleetHint} events={{ hover: false, focus: true, touch: false }}>
      <UnstyledButton className={classes.tile} visibleFrom="lg" px={8} h={32} onClick={onOpenFleet} aria-keyshortcuts="g f">
        <VisuallyHidden>{fleet} </VisuallyHidden>
        <Group gap={6} wrap="nowrap" align="center">
          {machines.slice(0, maxDots).map(m => <Dot key={m.id} m={m} now={now} />)}
          {machines.length > maxDots && <Text fz={12} ff="monospace" c="dimmed" lh={1}>+{machines.length - maxDots}</Text>}
          {machines.length === 0 && <StackIcon size={16} aria-hidden style={{ color: "var(--driga-muted)" }} />}
        </Group>
      </UnstyledButton>
    </Tooltip>
    <Tooltip label={fleetHint}>
      <UnstyledButton className={classes.tile} hiddenFrom="lg" px={8} h={32} onClick={onOpenFleet}>
        <Group gap={5} wrap="nowrap" align="center" c="dimmed">
          <StackIcon size={16} aria-hidden />
          <VisuallyHidden>Fleet: </VisuallyHidden>
          <Text fz={12} ff="monospace" lh={1}>{machines.length}</Text>
          <VisuallyHidden> machine{machines.length === 1 ? "" : "s"} · {notes}</VisuallyHidden>
          {worstOverall && <Box w={6} h={6} aria-hidden style={{ borderRadius: "50%", background: stateVar(worstOverall) }} />}
        </Group>
      </UnstyledButton>
    </Tooltip>
  </>;
}
