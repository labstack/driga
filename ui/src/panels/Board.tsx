import { Box, Group, Select, Stack, Text, Title } from "@mantine/core";
import { relative, exact } from "./format";
import { PanelView, type Act } from "./PanelView";
import type { Fragment, Panel } from "./types";

const defaultWidth: Record<Panel["viz"], number> = { stat: 3, gauge: 3, timeseries: 6, bar: 6, table: 12, list: 6, inbox: 8, cards: 12, timeline: 12, text: 6 };

/** Board lays a fragment's panels on the 12-column grid, as chat cards show
 * them. Positions saved by dragging win; otherwise panels flow in order. */
export function Board({ fragment, onVars, act, compact, stacked, bare }: { fragment: Fragment; onVars?: (vars: Record<string, string>) => void; act?: Act; compact?: boolean;
  /** stacked puts every panel full width, one below another, for a narrow column. */
  stacked?: boolean;
  /** bare leaves out the board's own title line, for a card that has one. */
  bare?: boolean }) {
  const { dashboard } = fragment;
  const results = new Map(fragment.results.map(r => [r.id, r]));
  const vars = fragment.vars ?? {};
  return <Stack gap="sm" p={bare ? 0 : compact ? "xs" : "md"}>
    {!bare && <Group justify="space-between" align="flex-end" gap="xs">
      <Box>
        <Title order={1} fz="md" fw={600}>{dashboard.name}</Title>
        {dashboard.description && !compact && <Text size="sm" c="dimmed">{dashboard.description}</Text>}
      </Box>
      <Group gap="xs">
        {onVars && dashboard.variables?.map(v => <Select key={v.name} size="xs" w={140} label={v.name} placeholder="All" clearable
          data={fragment.options?.[v.name] ?? []} value={vars[v.name] || null} onChange={next => onVars({ ...vars, [v.name]: next ?? "" })} />)}
        <Text size="xs" c="dimmed" title={exact(fragment.observed_at)}>observed {relative(fragment.observed_at)}</Text>
      </Group>
    </Group>}
    <Box style={{ display: "grid", gridTemplateColumns: "repeat(12, minmax(0, 1fr))", gap: 12, gridAutoFlow: "row dense" }}>
      {dashboard.panels.map(p => {
        const w = stacked ? 12 : Math.min(12, p.grid?.w ?? p.width ?? defaultWidth[p.viz]);
        return <Box key={p.id} style={{ gridColumn: `span ${w}`, minWidth: 0 }}>
          <PanelView panel={p} result={results.get(p.id)} act={act} />
        </Box>;
      })}
    </Box>
  </Stack>;
}
