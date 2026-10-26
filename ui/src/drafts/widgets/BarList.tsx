// BarList ranks items by a value: each row is a soft accent-tinted bar as
// long as the item's share of the largest, with the name on the bar and the
// value right-aligned in mono. Tremor's bar list built from Mantine alone,
// since @mantine/charts' BarsList would bring a second chart engine. Rows
// are buttons when the list offers a drill-down.
import { Box, Group, Stack, Text, UnstyledButton } from "@mantine/core";
import "./widgets.css";

export type BarItem = { id: string; label: string; value: number; /** A second fact beside the name: "3 sessions". */ sub?: string };

export type BarListProps = {
  items: BarItem[];
  format: (n: number) => string;
  /** What is ranked, for the list's accessible name. */
  label: string;
  /** Bars are measured against this rather than the largest item. */
  max?: number;
  onSelect?: (id: string) => void;
  /** Show 1, 2, 3 before the names. */
  ranked?: boolean;
};

export function BarList({ items, format, label, max, onSelect, ranked }: BarListProps) {
  const top = max ?? Math.max(0, ...items.map(i => i.value));
  return <Stack gap={4} role="list" aria-label={label}>
    {items.map((it, i) => {
      const width = top > 0 ? `${Math.max(0, Math.min(100, (it.value / top) * 100))}%` : "0%";
      const row = <Group gap="sm" wrap="nowrap" align="center" className={onSelect ? "w-row" : undefined} data-clickable={onSelect ? "" : undefined} style={{ borderRadius: "var(--mantine-radius-sm)" }}>
        <Box pos="relative" style={{ flex: 1, minWidth: 0, height: 28 }}>
          <Box aria-hidden pos="absolute" top={0} left={0} h="100%" w={width} style={{ background: "color-mix(in srgb, var(--mantine-color-brand-filled) 8%, transparent)", borderRadius: "var(--mantine-radius-sm)", transition: "width 300ms ease-out" }} />
          <Group pos="relative" gap={8} h={28} px={8} wrap="nowrap" style={{ minWidth: 0 }}>
            {ranked && <Text size="xs" ff="monospace" c="dimmed" w={16} style={{ flex: "none" }}>{i + 1}</Text>}
            <Text size="sm" fw={500} truncate title={it.label} style={{ minWidth: 0 }}>{it.label}</Text>
            {it.sub && <Text size="xs" c="dimmed" truncate title={it.sub} style={{ minWidth: 0, flex: "0 1 auto" }}>{it.sub}</Text>}
          </Group>
        </Box>
        <Text size="sm" ff="monospace" ta="right" style={{ flex: "none", minWidth: 64, whiteSpace: "nowrap" }}>{format(it.value)}</Text>
      </Group>;
      return <Box key={it.id} role="listitem">
        {onSelect ? <UnstyledButton w="100%" onClick={() => onSelect(it.id)}>{row}</UnstyledButton> : row}
      </Box>;
    })}
  </Stack>;
}
