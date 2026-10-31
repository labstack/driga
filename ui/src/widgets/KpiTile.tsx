// KpiTile is a stat with context: a tracked label, a large mono value with
// its unit, a delta chip that says the change over a window, and a
// sparkline. A zero is quiet: grey value, no chip, no sparkline, no tint. A
// count that needs the person tints the tile in its state color with a 2 px
// left rule and a dot; the waiting dot may breathe. Clickable tiles are
// buttons and show their selection as an inset ring. On a board the tile is
// a bordered card; in the header it is a bare 56 px cluster with the same
// tint and rule. A tile's menu waits in its corner, or opens on right-click.
import { ActionIcon, Box, ColorSwatch, Group, Menu, Stack, Text, Tooltip, UnstyledButton, useComputedColorScheme, VisuallyHidden, type MantineBreakpoint } from "@mantine/core";
import { DotsThree, type Icon } from "@phosphor-icons/react";
import { useState, type MouseEvent, type ReactNode } from "react";
import { value as format } from "../panels/format";
import { tones, type Tone } from "../theme/tokens";
import { DeltaChip, type Polarity } from "./DeltaChip";
import { Measure, Sparkline } from "./Measure";
import "./widgets.css";

export type Attention = "waiting" | "review" | "stuck";

/** Which direction is good for this metric: up for working sessions, down for memory, neither for a count of turns. */
export type Direction = "up-good" | "down-good" | "neutral";

export type Delta = { change: number; window: string };

export type MenuItem = { label: string; icon?: Icon; onClick: () => void; disabled?: boolean; hint?: string };

export type KpiTileProps = {
  label: string;
  /** A name after the label, in mono and its own case: the machine under "Memory". */
  sublabel?: string;
  icon?: Icon;
  value: number | null;
  /** How the value reads: bytes, count, percent, duration. */
  unit?: string;
  /** The value's whole, shown as 25 / 36 GB. */
  total?: number;
  /** A caption after the value in mono: "1.8 GB" after a count of trees. */
  caption?: string;
  /** Hidden text completing the tile's name: "process trees". */
  detail?: string;
  delta?: Delta;
  direction?: Direction;
  /** Readings over the window, newest last, for the sparkline. */
  trend?: (number | null)[];
  /** The kind of attention a count above zero means; the tile tints in its color. */
  attention?: Attention;
  /** What the tile means, on hover. */
  description?: string;
  /** A richer tooltip, with a key or the change in words. */
  tooltip?: ReactNode;
  selected?: boolean;
  onClick?: () => void;
  /** The key that reaches the tile, as aria-keyshortcuts. */
  shortcut?: string;
  size?: "board" | "header";
  /** Actions behind the tile's ⋯, also on right-click. */
  menu?: MenuItem[];
  /** Below the value row: a pressure bar under memory. */
  children?: ReactNode;
  visual?: ReactNode;
  menuItem?: boolean;
  visibleFrom?: MantineBreakpoint;
  hiddenFrom?: MantineBreakpoint;
};

const attentionTone: Record<Attention, Tone> = { waiting: "warning", review: "info", stuck: "danger" };

/** polarityOf says whether a change is good news for a metric. */
export function polarityOf(change: number, direction: Direction): Polarity {
  if (direction === "neutral" || Math.abs(change) < 1e-9) return "neutral";
  return (change > 0) === (direction === "up-good") ? "good" : "bad";
}

export function KpiTile({ label, sublabel, icon: Glyph, value, unit, total, caption, detail, delta, direction = "neutral", trend, attention, description, tooltip, selected, onClick, shortcut, size = "board", menu, children, visual, menuItem, visibleFrom, hiddenFrom }: KpiTileProps) {
  const scheme = useComputedColorScheme("dark");
  const [open, setOpen] = useState(false);
  const zero = value === null || value === 0;
  const tone = attention && typeof value === "number" && value > 0 ? attentionTone[attention] : undefined;
  const header = size === "header";
  const chip = delta && !zero ? <DeltaChip delta={delta.change} amount={format(Math.abs(delta.change), unit)} polarity={polarityOf(delta.change, direction)} window={delta.window} /> : null;
  // The sparkline draws on a canvas, which reads no CSS variable: the tone resolved per scheme, or the accent.
  const line = trend && trend.length > 1 && !zero ? <Sparkline trend={trend} title={label} color={tones[scheme][tone ?? "brand"]} /> : null;
  const Root = menuItem ? Menu.Item : onClick ? UnstyledButton : Box;
  const onContext = (e: MouseEvent) => { if (menu?.length) { e.preventDefault(); setOpen(true); } };
  const tile = <Root className="w-tile" data-size={size} data-clickable={onClick ? "" : undefined} data-selected={selected ? "true" : undefined} data-attention={tone}
    aria-pressed={!menuItem && onClick && selected !== undefined ? selected : undefined} aria-keyshortcuts={shortcut} onClick={onClick} onContextMenu={onContext} title={description} px={header ? 8 : "sm"} py={header ? 1 : "sm"}>
    {visual ?? <Stack gap={header ? 2 : 4}>
      <Group gap={6} wrap="nowrap" c={tone ? tone : "var(--driga-faint)"}>
        {Glyph && <Glyph size={14} aria-hidden style={{ flex: "none" }} />}
        <Text size="xs" fw={500} tt="uppercase" lts="0.04em" c={tone ? "var(--driga-text)" : "dimmed"} lh={header ? 1.4 : 1.2} truncate title={sublabel ? `${label} · ${sublabel}` : label} style={{ whiteSpace: "nowrap" }}>
          {label}{sublabel && <> · <Text span fz="xs" fw={500} ff="monospace" tt="none" lts={0} lh="inherit" c="dimmed">{sublabel}</Text></>}
        </Text>
      </Group>
      <Group gap={header ? 6 : 6} wrap="nowrap" align="center">
        {tone && <ColorSwatch size={6} color={`var(--mantine-color-${tone}-filled)`} withShadow={false} aria-hidden className={attention === "waiting" ? "w-pulse" : undefined} style={{ flex: "none" }} />}
        {/* Plex Mono has taller text bounds on Linux; leave room above the value without growing the 56 px header. */}
        <Box c={zero ? "dimmed" : undefined} style={{ whiteSpace: "nowrap" }}><Measure n={value} unit={unit} total={total} size={header ? 20 : 22} lineHeight={header ? 1.4 : 1.1} /></Box>
        {caption && <Text fz={12} ff="monospace" c="dimmed" lh={1.2} style={{ whiteSpace: "nowrap" }}>· {caption}</Text>}
        {detail && <VisuallyHidden> {detail}</VisuallyHidden>}
        {header && line && <Box w={72} h={20} aria-hidden style={{ flex: "none" }}>{line}</Box>}
        {header && chip && <Box visibleFrom="lg">{chip}</Box>}
      </Group>
      {!header && (chip || line) && <Group justify="space-between" wrap="nowrap" gap={6} h={24}>
        {chip ?? <span />}
        {line && <Box h={24} style={{ flex: "1 1 48px", minWidth: 40, maxWidth: 96 }}>{line}</Box>}
      </Group>}
      {header && children ? <Box className="w-tile-foot">{children}</Box> : children}
    </Stack>}
  </Root>;
  return <Box className="w-tile-box" visibleFrom={visibleFrom} hiddenFrom={hiddenFrom} style={header ? { display: "inline-block" } : undefined}>
    {tooltip ? <Tooltip label={tooltip} events={{ hover: true, focus: true, touch: true }}>{tile}</Tooltip> : tile}
    {menu && menu.length > 0 && <Menu opened={open} onChange={setOpen} position="bottom-end" withinPortal={!menuItem} shadow="md" width={200}>
      <Menu.Target>
        <ActionIcon className="w-tile-menu" size={18} aria-label={`More for ${label}`}><DotsThree size={14} /></ActionIcon>
      </Menu.Target>
      <Menu.Dropdown>
        {menu.map(item => {
          const I = item.icon;
          const action = <Menu.Item key={item.label} disabled={item.disabled} leftSection={I ? <I /> : undefined} onClick={item.onClick}>{item.label}</Menu.Item>;
          return item.hint ? <Tooltip key={item.label} label={item.hint} events={{ hover: true, focus: true, touch: true }}><Box tabIndex={0}>{action}</Box></Tooltip> : action;
        })}
      </Menu.Dropdown>
    </Menu>}
  </Box>;
}
