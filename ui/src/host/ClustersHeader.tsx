import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors } from "@dnd-kit/core";
import { SortableContext, arrayMove, horizontalListSortingStrategy, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { ActionIcon, Box, Button, Divider, Group, Kbd, Menu, Progress, Text, Tooltip, UnstyledButton, VisuallyHidden } from "@mantine/core";
import { useElementSize } from "@mantine/hooks";
import { ChatCircle, DotsSixVertical, MagnifyingGlass, SquaresFour, Stack as StackIcon } from "@phosphor-icons/react";
import { useCallback, useLayoutEffect, useState, type ReactNode } from "react";
import { value as format } from "../panels/format";
import type { Panel } from "../panels/types";
import { pressureTone } from "../widgets/BarGauge";
import { KpiTile, type MenuItem } from "../widgets/KpiTile";
import { RingGauge } from "../widgets/RingGauge";
import { stateVar } from "../widgets/StatusBadge";
import { Brand } from "./Brand";
import { changeWords, fleetSummary, kinds, machineSummary, percent, worst, type HeaderTile, type HeaderProps, type Machine } from "./Header";
import classes from "./Header.module.css";
import { captionOf } from "./headerTiles";

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
const tileGap = 6;

function tileMenu(panel: Panel, onShow: HeaderProps["onShow"], onAsk?: HeaderProps["onAsk"], onRemove?: HeaderProps["onRemove"], last?: boolean, busy?: boolean): MenuItem[] {
  return [{ label: "Show on Home", icon: SquaresFour, onClick: () => onShow(panel) },
    ...(onAsk ? [{ label: "Ask Chat about it", icon: ChatCircle, onClick: () => onAsk(panel.title) }] : []),
    ...(onRemove ? [{ label: "Remove from header", disabled: last || busy, hint: last ? "The header needs one widget" : undefined, onClick: () => onRemove(panel.id) }] : [])];
}

function SortableTile({ tile, enabled, children }: { tile: HeaderTile; enabled: boolean; children: ReactNode }) {
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition } = useSortable({ id: tile.id, disabled: !enabled });
  return <Box ref={setNodeRef} data-header-tile={tile.id} data-reorderable={enabled || undefined} className={classes.slot}
    onPointerDown={event => {
      // The value/label surface drags; the separate menu keeps its own pointer interaction.
      if ((event.target as HTMLElement).closest(".w-tile")) listeners?.onPointerDown?.(event);
    }}
    style={{ transition, transform: transform ? `translate3d(${transform.x}px, ${transform.y}px, 0)` : undefined, zIndex: transform ? 1 : undefined }}>
    {children}
    {enabled && <ActionIcon ref={setActivatorNodeRef} className={classes.drag} size={16} {...attributes} {...listeners} aria-label={`Reorder ${tile.title}`}><DotsSixVertical size={12} /></ActionIcon>}
  </Box>;
}

/** Measurements live outside the flex row, so hiding a tile cannot change its
 * measured width. Font loads, values and breakpoint changes are observed too. */
function Measured({ id, onWidth, children }: { id: string; onWidth: (id: string, width: number) => void; children: ReactNode }) {
  const { ref, width } = useElementSize({ box: "border-box" });
  useLayoutEffect(() => { if (width > 0) onWidth(id, Math.ceil(width)); }, [id, width, onWidth]);
  return <Box ref={ref} className={classes.slot}>{children}</Box>;
}

/** The stored tiles flow into the space between the mark and fleet controls. */
export function ClustersHeader({ data, filter, chatOpen, onFilter, onOpenFleet, onSearch, onToggleChat, onShow, onAsk, search, editor, busy, onRemove, onMove }: HeaderProps) {
  const { ref, width } = useElementSize();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));
  const tiles = data?.tiles ?? [];
  const [widths, setWidths] = useState<Record<string, number>>({});
  const onWidth = useCallback((id: string, width: number) => {
    setWidths(old => old[id] === width ? old : { ...old, [id]: width });
  }, []);
  // Measure every possible label: proportional digits need not have the
  // same width. Reserve the maximum so changing +9 to +10 is stable too.
  const overflowLabels = Array.from({ length: Math.max(1, tiles.length) }, (_, i) => `+${i + 1}`);
  const moreWidth = Math.max(0, ...overflowLabels.map(id => widths[id] ?? 0));
  let count = 0, used = 0;
  // Round the budget down and measurements up: fractional font widths must
  // never let a trailing pixel overlap the fixed controls.
  for (let i = 0; i < tiles.length; i++) {
    const tileWidth = widths[tiles[i].id];
    if (!tileWidth || !moreWidth) break;
    const next = used + (i ? tileGap : 0) + tileWidth;
    if (next + (i < tiles.length - 1 ? tileGap + moreWidth : 0) > Math.floor(width)) break;
    used = next; count = i + 1;
  }
  const hidden = tiles.slice(count);
  const renderTile = (tile: HeaderTile, overflow = false, measuring = false) => {
    const kind = tile.behavior.attention;
    const trend = tile.trend;
    const first = trend?.[0], last = trend?.[trend.length - 1];
    const delta = typeof first === "number" && typeof last === "number" && trend!.length > 1 ? { change: last - first, window: "1h" } : undefined;
    const pct = tile.value !== null && tile.total != null ? percent(tile.value, tile.total) : null;
    const totalBar = tile.viz === "stat" && pct !== null;
    const tooltip = tile.error ?? (kind ? `${kinds[kind].hint} · ${changeWords({ count: tile.value ?? 0, trend: trend ?? [], delta: delta?.change ?? 0 })}` : tile.panel.description ?? tile.title);
    const content = <KpiTile menuItem={overflow} size="header" label={tile.title} sublabel={tile.behavior.sublabel ? data?.localMachine : undefined} icon={tile.behavior.icon}
        value={tile.value} unit={tile.unit} total={tile.total ?? undefined} caption={tile.caption != null ? captionOf(tile.panel, tile.caption) : undefined}
        trend={totalBar ? undefined : trend} delta={totalBar ? undefined : delta} direction={tile.behavior.direction} attention={kind} selected={kind ? filter === kind : undefined}
        shortcut={!measuring && kind ? kinds[kind].key : undefined} tooltip={measuring ? undefined : tooltip} onClick={measuring ? undefined : () => kind ? onFilter(kind) : onShow(tile.panel)}
        menu={measuring ? undefined : tileMenu(tile.panel, onShow, onAsk, onRemove, tiles.length === 1, busy)}
        visual={tile.viz === "gauge" && tile.value !== null ? <RingGauge value={tile.value} max={tile.max ?? 100} format={n => format(n, tile.unit)} label={tile.behavior.sublabel ? `${tile.title} · ${data?.localMachine}` : tile.title} size={36}
          // The ring already shows a share of 100 in its centre; "42% / 100%" beside it would repeat it.
          caption={tile.unit === "percent" && (tile.max ?? 100) === 100 ? "" : undefined} /> : undefined}>
        {totalBar && <Progress size="xs" value={pct} color={pressureTone(pct / 100, [0.9, 0.9])} aria-label={`${pct}% of ${tile.title.toLowerCase()} used`} styles={{ root: { background: "var(--driga-border)" } }} />}
      </KpiTile>;
    return measuring ? <Measured key={tile.id} id={tile.id} onWidth={onWidth}>{content}</Measured> :
      <SortableTile key={tile.id} tile={tile} enabled={!!onMove && !busy}>{content}</SortableTile>;
  };
  const machines = data?.machines ?? [];
  return <Group h="100%" gap={8} wrap="nowrap" data-header-row data-header-ready={tiles.every(t => widths[t.id]) && overflowLabels.every(id => widths[id])}>
    <Brand />
    {data && <Rule />}
    <Box ref={ref} style={{ flex: 1, minWidth: moreWidth, position: "relative" }}>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={({ active, over }) => {
        if (!over || active.id === over.id || !onMove || busy) return;
        const from = tiles.findIndex(t => t.id === active.id), to = tiles.findIndex(t => t.id === over.id);
        if (from < 0 || to < 0) return;
        const next = arrayMove(tiles, from, to);
        onMove(String(active.id), next[to + 1]?.id);
      }}>
      <SortableContext items={tiles.map(t => t.id)} strategy={horizontalListSortingStrategy}>
      <Group gap={tileGap} wrap="nowrap">
        {tiles.slice(0, count).map(tile => renderTile(tile))}
        {hidden.length > 0 && <Menu position="bottom-end" width="auto" withinPortal shadow="md">
          <Menu.Target><Button className={classes.more} miw={moreWidth || undefined} variant="subtle" size="compact-sm" aria-label={`+${hidden.length}`}>+{hidden.length}</Button></Menu.Target>
          <Menu.Dropdown mah="70vh" style={{ overflowY: "auto" }}><SortableContext items={hidden.map(t => t.id)} strategy={verticalListSortingStrategy}>{hidden.map(tile => renderTile(tile, true))}</SortableContext></Menu.Dropdown>
        </Menu>}
      </Group>
      </SortableContext>
      </DndContext>
      <Box className={classes.measuring} aria-hidden="true" inert>
        {/* Passive copies keep intrinsic sizing without duplicate controls or portals. */}
        {tiles.map(tile => renderTile(tile, false, true))}
        {overflowLabels.map(label => <Measured key={label} id={label} onWidth={onWidth}><Button component="div" className={classes.more} variant="subtle" size="compact-sm">{label}</Button></Measured>)}
      </Box>
    </Box>
    {editor}
    {data && <Tooltip label={fleetSummary(machines)}>
      <UnstyledButton className={classes.tile} px={8} h={32} onClick={onOpenFleet} aria-label={fleetSummary(machines)} aria-keyshortcuts="g f">
        <Group gap={6} wrap="nowrap">
          {machines.slice(0, maxDots).map(m => <Dot key={m.id} m={m} now={Date.now()} />)}
          {machines.length === 0 && <StackIcon size={16} aria-hidden />}
          <Text fz={12} ff="monospace" c="dimmed">{machines.length}</Text>
        </Group>
      </UnstyledButton>
    </Tooltip>}
    {search ?? <UnstyledButton className="search-button" onClick={onSearch} aria-keyshortcuts="Meta+K" title="Search sessions and dashboards (⌘K)">
      <Group gap={8} wrap="nowrap"><MagnifyingGlass size={16} aria-hidden /><VisuallyHidden>Search</VisuallyHidden><Kbd size="xs" aria-hidden>⌘K</Kbd></Group>
    </UnstyledButton>}
    <Tooltip label={chatOpen ? "Hide Chat" : "Show Chat"}>
      <ActionIcon size="lg" variant={chatOpen ? "light" : "subtle"} color={chatOpen ? "brand" : "gray"} aria-pressed={chatOpen} aria-label="Chat" onClick={onToggleChat}><ChatCircle size={20} /></ActionIcon>
    </Tooltip>
  </Group>;
}
