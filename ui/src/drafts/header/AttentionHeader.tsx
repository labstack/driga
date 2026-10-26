import { ActionIcon, Box, Group, Kbd, Progress, Stack, Text, Tooltip, UnstyledButton, useComputedColorScheme, VisuallyHidden } from "@mantine/core";
import { Broom, ChatCircle, MagnifyingGlass, Memory, TreeStructure, type Icon } from "@phosphor-icons/react";
import type { CSSProperties, ReactNode } from "react";
import { Brand } from "../../host/Brand";
import { bytes } from "../../panels/format";
import { Sparkline } from "../../panels/PanelView";
import { tones } from "../../theme/tokens";
import { DeltaChip } from "./DeltaChip";
import { attentionKinds, kinds, machineDetail, machineSummary, percent, polarity, pressure, worst, type Attention, type AttentionKind, type HeaderProps, type Machine, type Poll } from "./Header";
import classes from "./Header.module.css";

/** A count tile: the number large, its last hour faded behind it, its
 * change beside it. The icon takes the kind's tone above zero; the number
 * stays in the text color so it reads in both schemes. Its label, number
 * and change name it. */
function Count({ kind, a, color, pressed, onClick }: { kind: AttentionKind; a: Attention; color: string; pressed: boolean; onClick: () => void }) {
  const k = kinds[kind];
  const live = a.count > 0;
  const Glyph = k.icon;
  const style = { boxShadow: pressed ? "inset 0 0 0 1px var(--mantine-color-brand-filled)" : undefined, overflow: "hidden" } as CSSProperties;
  const change = a.delta === 0 ? "steady over the last hour" : `${a.delta > 0 ? "up" : "down"} ${Math.abs(a.delta)} in the last hour`;
  return <Tooltip label={`${k.hint} · ${change}`}>
    <UnstyledButton className={classes.tile} pos="relative" miw={100} px={10} py={4} aria-pressed={pressed} onClick={onClick} style={style}>
      <Box pos="absolute" top="42%" bottom={0} left={0} right={0} aria-hidden style={{ opacity: 0.22, pointerEvents: "none" }}><Sparkline trend={a.trend} title={k.label} color={color} /></Box>
      <Stack gap={3} pos="relative">
        <Group gap={5} wrap="nowrap" c={live ? k.tone : "var(--driga-faint)"}>
          <Glyph size={14} aria-hidden />
          <Text fz={12} lh={1.2} c="dimmed" style={{ whiteSpace: "nowrap" }}>{k.label}</Text>
        </Group>
        <Group gap={6} wrap="nowrap" align="baseline">
          <Text ff="monospace" fz={22} fw={600} lh={1.1} c={live ? undefined : "dimmed"} style={{ fontVariantNumeric: "tabular-nums" }}>{a.count}</Text>
          <DeltaChip delta={a.delta} amount={String(Math.abs(a.delta))} polarity={polarity(kind, a.delta)} />
          <VisuallyHidden> {a.delta === 0 ? "over" : "in"} the last hour</VisuallyHidden>
        </Group>
      </Stack>
    </UnstyledButton>
  </Tooltip>;
}

/** A quiet resource: an icon and a mono value. The hidden label and the
 * value name it; the tooltip says the same. */
function Quiet({ icon: Glyph, label, hint, dim, onClick, children, ...rest }: { icon: Icon; label: string; hint: string; dim?: boolean; onClick: () => void; children: ReactNode; visibleFrom?: string }) {
  return <Tooltip label={hint}>
    <UnstyledButton className={classes.tile} px={6} h={28} onClick={onClick} {...rest}>
      <Group gap={5} wrap="nowrap" align="center" c={dim ? "dimmed" : undefined}>
        <Box c="var(--driga-faint)" style={{ display: "flex" }}><Glyph size={14} aria-hidden /></Box>
        <VisuallyHidden>{label} </VisuallyHidden>
        <Box fz={12} ff="monospace" lh={1} style={{ whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}>{children}</Box>
      </Group>
    </UnstyledButton>
  </Tooltip>;
}

const pollFill: Record<Poll, string> = {
  working: "var(--mantine-color-success-filled)",
  waiting: "var(--mantine-color-warning-filled)",
  stuck: "var(--mantine-color-danger-filled)",
  idle: "var(--mantine-color-info-filled)",
  quiet: "var(--driga-border)",
  offline: "transparent",
};

/** A machine chip: its name, memory share and the last 30 polls as a
 * tracker strip. It turns amber when a session there waits on the person
 * and red when one is stuck; unreachable machines are dashed. Its name,
 * share and hidden detail name it. */
function MachineChip({ m, now, onClick, ...rest }: { m: Machine; now: number; onClick: () => void; visibleFrom?: string }) {
  const state = worst(m);
  const tint = !m.reachable ? undefined : state === "stuck" ? "color-mix(in srgb, var(--mantine-color-danger-filled) 12%, transparent)" : state === "waiting" ? "color-mix(in srgb, var(--mantine-color-warning-filled) 12%, transparent)" : undefined;
  const style = { "--chip-bg": tint, borderStyle: m.reachable ? "solid" : "dashed", outline: m.local ? "1px solid var(--mantine-color-brand-filled)" : undefined, outlineOffset: 1 } as CSSProperties;
  return <Tooltip label={machineSummary(m, now)}>
    <UnstyledButton className={classes.chip} px={7} py={4} onClick={onClick} style={style} {...rest}>
      <Stack gap={4}>
        <Group gap={8} wrap="nowrap" justify="space-between">
          <Text fz={12} ff="monospace" lh={1} c={m.reachable ? undefined : "dimmed"} style={{ whiteSpace: "nowrap" }}>{m.name}</Text>
          <Text fz={12} ff="monospace" lh={1} c={tint ? undefined : "dimmed"} style={{ whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}>{percent(m.memory.used, m.memory.total)}%</Text>
          <VisuallyHidden> memory · {machineDetail(m, now)}</VisuallyHidden>
        </Group>
        <Group gap={1} wrap="nowrap" aria-hidden>
          {m.polls.map((p, i) => <Box key={i} w={2} h={6} style={{ borderRadius: 1, background: pollFill[p], boxShadow: p === "offline" ? "inset 0 0 0 1px var(--driga-faint)" : undefined, flex: "none" }} />)}
        </Group>
      </Stack>
    </UnstyledButton>
  </Tooltip>;
}

/** The machines past the chips, as one more chip that opens the Fleet. */
function MoreChip({ n, onClick, ...rest }: { n: number; onClick: () => void; visibleFrom?: string; hiddenFrom?: string }) {
  return <UnstyledButton className={classes.chip} px={8} h={36} onClick={onClick} {...rest}>
    <Text fz={12} ff="monospace" c="dimmed" lh={1}>+{n}</Text>
    <VisuallyHidden> more machine{n === 1 ? "" : "s"}, open Fleet</VisuallyHidden>
  </UnstyledButton>;
}

/** How many chips fit beside the counts: three on a desktop window, one on a laptop. */
const chipsWide = 3;
const chipsNarrow = 1;

/** AttentionHeader is the header as an attention bar: three large counts
 * with their last hour behind them, resources demoted to a quiet cluster on
 * the right, one chip per machine with a 30-poll tracker, and the bar one
 * surface step dimmer than the content, with no rule under it. */
export function AttentionHeader({ attention, hygiene, memory, machines, filter, chatOpen, onFilter, onOpenFleet, onSearch, onToggleChat, onShow }: HeaderProps) {
  const scheme = useComputedColorScheme("dark");
  const now = Date.now();
  const pct = percent(memory.used, memory.total);
  const leftover = hygiene.leftover;
  const more = (shown: number) => Math.max(0, machines.length - shown);

  return <Box component="header" h={56} px="md" style={{ background: "var(--driga-inset)" }}>
    <Group h="100%" gap={8} wrap="nowrap">
      <Brand />
      <Group gap={6} wrap="nowrap" ml={8} role="group" aria-label="Attention">
        {attentionKinds.map(kind => <Count key={kind} kind={kind} a={attention[kind]} color={tones[scheme][kinds[kind].tone]} pressed={filter === kind} onClick={() => onFilter(kind)} />)}
      </Group>

      <Box style={{ flex: 1 }} />

      <Group gap={2} wrap="nowrap" role="group" aria-label="Resources">
        <Quiet visibleFrom="lg" icon={TreeStructure} label="Leftover process trees:" hint={`Leftover process trees: ${leftover.count}${leftover.count ? `, ${bytes(leftover.bytes)}` : ""}`} dim={!leftover.count} onClick={() => onShow("leftover")}>
          {leftover.count}{leftover.count > 0 && <Text span fz={12} ff="monospace" c="dimmed"> · {bytes(leftover.bytes)}</Text>}
        </Quiet>
        <Quiet visibleFrom="lg" icon={Broom} label="Reclaimable:" hint={`Reclaimable: ${bytes(hygiene.reclaimable)}`} dim={!hygiene.reclaimable} onClick={() => onShow("reclaim")}>{bytes(hygiene.reclaimable)}</Quiet>
        <Quiet icon={Memory} label={`Memory on ${memory.machine}:`} hint={`Memory on ${memory.machine}: ${bytes(memory.used)} of ${bytes(memory.total)}, ${pct}%`} onClick={() => onShow("memory")}>
          <Group gap={6} wrap="nowrap" align="center">
            <span>{bytes(memory.used).replace(/ \w+$/, "")}<Text span fz={12} ff="monospace" c="dimmed"> / {bytes(memory.total)}</Text></span>
            <Progress size="xs" w={40} value={pct} color={pressure(pct)} aria-label={`${pct}% of memory used`} styles={{ root: { background: "var(--driga-border)" } }} />
          </Group>
        </Quiet>
      </Group>

      <Group gap={6} wrap="nowrap" role="group" aria-label="Machines">
        {machines.slice(0, chipsWide).map((m, i) => <MachineChip key={m.name} m={m} now={now} onClick={onOpenFleet} visibleFrom={i >= chipsNarrow ? "lg" : undefined} />)}
        {more(chipsWide) > 0 && <MoreChip visibleFrom="lg" n={more(chipsWide)} onClick={onOpenFleet} />}
        {more(chipsNarrow) > 0 && <MoreChip hiddenFrom="lg" n={more(chipsNarrow)} onClick={onOpenFleet} />}
      </Group>

      <UnstyledButton className="search-button" onClick={onSearch} aria-keyshortcuts="Meta+K" title="Search sessions and dashboards (⌘K)">
        <Group gap={8} wrap="nowrap">
          <MagnifyingGlass size={16} aria-hidden />
          <VisuallyHidden>Search sessions and dashboards</VisuallyHidden>
          <Text size="sm" c="dimmed" visibleFrom="lg" aria-hidden>Search</Text>
          <Kbd size="sm" visibleFrom="lg" aria-hidden>⌘K</Kbd>
        </Group>
      </UnstyledButton>
      <Tooltip label={chatOpen ? "Hide Chat" : "Show Chat"}>
        <ActionIcon size={32} variant={chatOpen ? "light" : "subtle"} color={chatOpen ? "brand" : "gray"} aria-pressed={chatOpen} aria-label="Chat" onClick={onToggleChat}><ChatCircle size={20} /></ActionIcon>
      </Tooltip>
    </Group>
  </Box>;
}
