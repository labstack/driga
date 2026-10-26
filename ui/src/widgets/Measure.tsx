import { Text } from "@mantine/core";
import { useCallback } from "react";
import { Chart } from "../panels/Chart";
import { sparkline as sparklineOption, type Theme } from "../panels/charts";
import { value } from "../panels/format";
import type { Cell } from "../panels/types";

/** Measure shows a number with its unit at a smaller size, as Canon's stat
 * tile does: "1.8" then "GB". */
export function Measure({ n, unit, size = 28, total }: { n: Cell; unit?: string; size?: number; total?: Cell }) {
  if (typeof n !== "number") return <Text ff="monospace" fz={size} fw={600} lh={1.1} c="dimmed">–</Text>;
  const [number, suffix] = splitUnit(value(n, unit));
  // Out of a total, the value and the total share the total's unit: 25 / 32 GB.
  if (typeof total === "number") {
    const [whole, wholeSuffix] = splitUnit(value(total, unit));
    const [part] = splitUnit(unit === "bytes" ? sameUnit(n, total) : value(n, unit));
    return <Text ff="monospace" fz={size} fw={600} lh={1.1} style={{ fontVariantNumeric: "tabular-nums" }}>
      {part}<Text span ff="monospace" fz="0.6em" fw={500} c="dimmed" mx={3}>/ {whole}</Text>{wholeSuffix && <Text span ff="monospace" fz="0.6em" fw={500} c="dimmed">{wholeSuffix}</Text>}
    </Text>;
  }
  return <Text ff="monospace" fz={size} fw={600} lh={1.1} style={{ fontVariantNumeric: "tabular-nums" }}>
    {number}{suffix && <Text span ff="monospace" fz="0.6em" fw={500} c="dimmed" ml={3}>{suffix}</Text>}
  </Text>;
}

// sameUnit writes n in the unit total is written in, so 25 GB / 32 GB reads
// as 25 / 32 GB.
function sameUnit(n: number, total: number): string {
  const steps = Math.max(0, Math.min(4, Math.floor(Math.log(Math.max(total, 1)) / Math.log(1024))));
  const scaled = n / 1024 ** steps;
  return scaled >= 10 ? String(Math.round(scaled)) : scaled.toFixed(1);
}

function splitUnit(text: string): [string, string] {
  const m = /^([$]?[\d.,]+)\s*(.*)$/.exec(text);
  return m ? [m[1], m[2]] : [text, ""];
}

/** Sparkline is a trend beside a number: no axes, the last point marked,
 * in the accent unless the metric has a color of its own. */
export function Sparkline({ trend, title, color, window = "hour" }: { trend: (number | null)[]; title: string; color?: string; window?: string }) {
  const option = useCallback((t: Theme) => sparklineOption(trend, title, t, color, window), [trend, title, color, window]);
  return <Chart option={option} label={`${title} over the last ${window}`} small />;
}
