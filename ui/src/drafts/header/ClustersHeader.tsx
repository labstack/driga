import { ActionIcon, Box, Divider, Group, Kbd, Progress, Stack, Text, Tooltip, UnstyledButton, useComputedColorScheme, VisuallyHidden } from "@mantine/core";
import { Broom, ChatCircle, CheckCircle, MagnifyingGlass, Memory, Stack as StackIcon, TreeStructure, type Icon } from "@phosphor-icons/react";
import { useEffect, useRef, type CSSProperties, type ReactNode } from "react";
import { Brand } from "../../host/Brand";
import { bytes, exact } from "../../panels/format";
import { Measure, Sparkline } from "../../panels/PanelView";
import { tones } from "../../theme/tokens";
import { DeltaChip } from "./DeltaChip";
import { attentionKinds, clock, fleetNotes, fleetSummary, kinds, machineSummary, percent, polarity, pressure, stateTone, worst, type AttentionKind, type HeaderProps, type Machine } from "./Header";
import classes from "./Header.module.css";

/** The tiles' label register: small, tracked uppercase, muted unless the
 * tile is tinted, where the text color keeps it readable. */
function Label({ children, strong }: { children: ReactNode; strong?: boolean }) {
  return <Text fz={12} fw={500} lh={1.2} tt="uppercase" lts="0.04em" c={strong ? "var(--driga-text)" : "dimmed"} style={{ whiteSpace: "nowrap" }}>{children}</Text>;
}

/** A hairline between clusters. */
function Rule() {
  return <Divider orientation="vertical" h={24} color="var(--driga-border)" style={{ alignSelf: "center" }} />;
}

/** An attention tile: quiet at zero, tinted with a rule and a dot above
 * it. Only the waiting dot pulses. Pressed while the inbox is filtered to
 * it. Its label and number name it. */
function AttentionTile({ kind, count, pressed, onClick, tileRef }: { kind: AttentionKind; count: number; pressed: boolean; onClick: () => void; tileRef: (el: HTMLButtonElement | null) => void }) {
  const k = kinds[kind];
  const live = count > 0;
  const Glyph = k.icon;
  // The tint and its hover are the tile's own variables, which the class reads.
  const style = {
    "--tile-bg": live ? `color-mix(in srgb, var(--mantine-color-${k.tone}-filled) 12%, transparent)` : undefined,
    "--tile-hover": live ? `color-mix(in srgb, var(--mantine-color-${k.tone}-filled) 20%, transparent)` : undefined,
    borderLeft: `2px solid ${live ? `var(--mantine-color-${k.tone}-filled)` : "transparent"}`,
    boxShadow: pressed ? "inset 0 0 0 1px var(--mantine-color-brand-filled)" : undefined,
  } as CSSProperties;
  return <Tooltip label={<Group gap={6} wrap="nowrap">{k.hint}<Kbd size="sm">{k.key}</Kbd></Group>}>
    <UnstyledButton ref={tileRef} className={classes.tile} miw={104} px={8} py={5} aria-pressed={pressed} aria-keyshortcuts={k.key} onClick={onClick} style={style}>
      <Stack gap={2}>
        <Group gap={6} wrap="nowrap" c={live ? k.tone : "var(--driga-faint)"}>
          <Glyph size={14} aria-hidden />
          <Label strong={live}>{k.label}</Label>
        </Group>
        <Group gap={6} wrap="nowrap" align="center">
          {live && <Box w={6} h={6} className={kind === "waiting" ? classes.pulse : undefined} aria-hidden style={{ borderRadius: "50%", background: `var(--mantine-color-${k.tone}-filled)`, flex: "none" }} />}
          <Box c={live ? undefined : "dimmed"}><Measure n={count} size={20} /></Box>
        </Group>
      </Stack>
    </UnstyledButton>
  </Tooltip>;
}

/** A hygiene tile: never tinted, these are chores. Its label, number and
 * caption name it; `detail` adds what they leave out, for screen readers. */
function HygieneTile({ icon: Glyph, label, hint, caption, detail, children, onClick, ...rest }: { icon: Icon; label: string; hint: string; caption?: string; detail?: string; children: ReactNode; onClick: () => void; visibleFrom?: string; hiddenFrom?: string }) {
  return <Tooltip label={hint}>
    <UnstyledButton className={classes.tile} miw={120} px={8} py={5} onClick={onClick} {...rest}>
      <Stack gap={2}>
        <Group gap={6} wrap="nowrap" c="var(--driga-faint)"><Glyph size={14} aria-hidden /><Label>{label}</Label></Group>
        <Group gap={4} wrap="nowrap" align="baseline">
          {children}
          {caption && <Text fz={12} ff="monospace" c="dimmed" lh={1.2} style={{ whiteSpace: "nowrap" }}>· {caption}</Text>}
          {detail && <VisuallyHidden> {detail}</VisuallyHidden>}
        </Group>
      </Stack>
    </UnstyledButton>
  </Tooltip>;
}

/** A fleet dot: the machine's worst session state, hollow when
 * unreachable, ringed in the accent for this machine. */
function Dot({ m, now }: { m: Machine; now: number }) {
  const state = worst(m);
  const fill = !m.reachable ? "transparent" : state ? `var(--mantine-color-${stateTone[state]}-filled)` : "var(--driga-faint)";
  return <Tooltip label={machineSummary(m, now)} events={{ hover: true, focus: false, touch: true }}>
    <Box w={8} h={8} aria-hidden style={{ borderRadius: "50%", background: fill, flex: "none", boxShadow: !m.reachable ? "inset 0 0 0 1.5px var(--driga-faint)" : undefined, outline: m.local ? "1px solid var(--mantine-color-brand-filled)" : undefined, outlineOffset: 1 }} />
  </Tooltip>;
}

const maxDots = 8;

/** ClustersHeader is the header as four clusters set apart by hairlines:
 * attention, hygiene, this machine and the fleet, then search and Chat. A
 * count that needs the person takes its tone; zeros stay quiet; when nothing
 * needs them the attention tiles fold into one line. The keys 1, 2 and 3
 * focus the attention tiles and g then f opens the Fleet. */
export function ClustersHeader({ attention, hygiene, memory, machines, checkedAt, filter, chatOpen, onFilter, onOpenFleet, onSearch, onToggleChat, onShow }: HeaderProps) {
  const scheme = useComputedColorScheme("dark");
  const now = Date.now();
  const tiles = useRef<Partial<Record<AttentionKind, HTMLButtonElement | null>>>({});
  useEffect(() => {
    let g = -Infinity;
    const typing = (t: EventTarget | null) => t instanceof HTMLElement && (t.isContentEditable || /^(input|textarea|select)$/i.test(t.tagName));
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
      if (e.key === "g") { g = performance.now(); return; }
      if (e.key === "f" && performance.now() - g < 800) { g = -Infinity; onOpenFleet(); return; }
      const kind = attentionKinds[Number(e.key) - 1];
      if (kind) tiles.current[kind]?.focus();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onOpenFleet]);

  const clear = attentionKinds.every(k => attention[k].count === 0);
  const pct = percent(memory.used, memory.total);
  const memoryPolarity = polarity("memory", memory.delta, memory.total);
  const leftover = hygiene.leftover;
  const notes = fleetNotes(machines);
  const fleet = fleetSummary(machines);
  const worstOverall = machines.map(worst).find(s => s === "stuck") ?? machines.map(worst).find(s => s === "waiting") ?? null;
  const fleetHint = <Group gap={6} wrap="nowrap">{fleet}<Kbd size="sm">g f</Kbd></Group>;

  return <Box component="header" h={56} px="md" style={{ background: "var(--driga-raised)", borderBottom: "1px solid var(--driga-border)" }}>
    <Group h="100%" gap={8} wrap="nowrap">
      <Brand />
      <Rule />

      {clear
        ? <Group gap={8} wrap="nowrap" role="status" miw={240} px={8} py={4}>
          <Box w={20} h={20} c="success" style={{ borderRadius: "50%", background: "color-mix(in srgb, var(--mantine-color-success-filled) 14%, transparent)", display: "grid", placeItems: "center", flex: "none" }}><CheckCircle size={14} aria-hidden /></Box>
          <Stack gap={0}>
            <Text fz={13} fw={500} lh={1.2} style={{ whiteSpace: "nowrap" }}>Nothing needs you</Text>
            <Text fz={12} ff="monospace" c="dimmed" lh={1.2} title={exact(checkedAt)} style={{ whiteSpace: "nowrap" }}>checked {clock(checkedAt)}</Text>
          </Stack>
        </Group>
        : <Group gap={8} wrap="nowrap" role="group" aria-label="Attention">
          {attentionKinds.map(kind => <AttentionTile key={kind} kind={kind} count={attention[kind].count} pressed={filter === kind} onClick={() => onFilter(kind)} tileRef={el => { tiles.current[kind] = el; }} />)}
        </Group>}
      <Rule />

      <HygieneTile visibleFrom="lg" icon={TreeStructure} label="Leftover" hint="Process trees their sessions left behind" caption={leftover.count ? bytes(leftover.bytes) : undefined} detail="process trees" onClick={() => onShow("leftover")}>
        <Box c={leftover.count ? undefined : "dimmed"}><Measure n={leftover.count} size={20} /></Box>
      </HygieneTile>
      <HygieneTile visibleFrom="lg" icon={Broom} label="Reclaimable" hint="What a cleanup would free; opens the Reclaim panel" onClick={() => onShow("reclaim")}>
        <Box c={hygiene.reclaimable ? undefined : "dimmed"}><Measure n={hygiene.reclaimable} unit="bytes" size={20} /></Box>
      </HygieneTile>
      <HygieneTile hiddenFrom="lg" icon={Broom} label="Reclaim" hint={`${leftover.count} leftover process trees, ${bytes(leftover.bytes)}; a cleanup would free ${bytes(hygiene.reclaimable)}`} caption={bytes(hygiene.reclaimable)} detail={`leftover process trees, ${bytes(leftover.bytes)}; ${bytes(hygiene.reclaimable)} reclaimable`} onClick={() => onShow("reclaim")}>
        <Box c={leftover.count ? undefined : "dimmed"}><Measure n={leftover.count} size={20} /></Box>
      </HygieneTile>
      <Rule />

      <Tooltip label={`${bytes(memory.used)} of ${bytes(memory.total)} on ${memory.machine}; opens the machine`}>
        <UnstyledButton className={classes.tile} w={{ base: 184, lg: 284 }} px={8} py={3} onClick={() => onShow("memory")}>
          <Stack gap={2}>
            <Group gap={6} wrap="nowrap" c="var(--driga-faint)"><Memory size={14} aria-hidden /><Label>Memory · <Text span fz={12} fw={500} ff="monospace" tt="none" lts={0} c="dimmed">{memory.machine}</Text></Label></Group>
            <Group gap={8} wrap="nowrap" align="center">
              <Box style={{ whiteSpace: "nowrap" }}><Measure n={memory.used} unit="bytes" total={memory.total} size={20} /></Box>
              <Box w={72} h={20} aria-hidden style={{ flex: "none" }}><Sparkline trend={memory.trend} title="Memory" color={tones[scheme].brand} /></Box>
              <Box visibleFrom="lg"><DeltaChip delta={memory.delta} amount={bytes(Math.abs(memory.delta))} polarity={memoryPolarity} window="1h" /></Box>
            </Group>
            <Progress size="xs" value={pct} color={pressure(pct)} aria-label={`${pct}% of memory used`} styles={{ root: { background: "var(--driga-border)" } }} />
          </Stack>
        </UnstyledButton>
      </Tooltip>
      <Rule />

      <Tooltip label={fleetHint} events={{ hover: false, focus: true, touch: false }}>
        <UnstyledButton className={classes.tile} visibleFrom="lg" px={8} h={32} onClick={onOpenFleet}>
          <VisuallyHidden>{fleet} </VisuallyHidden>
          <Group gap={6} wrap="nowrap" align="center">
            {machines.slice(0, maxDots).map(m => <Dot key={m.name} m={m} now={now} />)}
            {machines.length > maxDots && <Text fz={12} ff="monospace" c="dimmed" lh={1}>+{machines.length - maxDots}</Text>}
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
            {worstOverall && <Box w={6} h={6} aria-hidden style={{ borderRadius: "50%", background: `var(--mantine-color-${stateTone[worstOverall]}-filled)` }} />}
          </Group>
        </UnstyledButton>
      </Tooltip>

      <Box style={{ flex: 1 }} />

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
