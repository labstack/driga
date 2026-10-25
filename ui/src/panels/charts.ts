// Compiles Driga panels to ECharts options. The model writes Driga's panel
// spec, never ECharts options: formatters, colors and layout stay here, and
// every string from the data is escaped before it reaches a tooltip.
import { BarChart, LineChart } from "echarts/charts";
import { AriaComponent, DataZoomComponent, GridComponent, TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer, SVGRenderer } from "echarts/renderers";
import { fonts, series as slots, surfaces, typeScale, type Scheme } from "../theme/tokens";
import { label, value } from "./format";
import type { Frame, Panel } from "./types";

echarts.use([BarChart, LineChart, GridComponent, TooltipComponent, DataZoomComponent, AriaComponent, CanvasRenderer, SVGRenderer]);

export { echarts };

/** Charts draw into a canvas that cannot read CSS variables, so they take
 * resolved colors from the same tokens the theme uses, per scheme. */
export type Theme = { text: string; muted: string; grid: string; surface: string; border: string; series: readonly string[] };

export function chartTheme(scheme: Scheme): Theme {
  const s = surfaces[scheme];
  return { text: s.text, muted: s.muted, grid: s.borderSoft, surface: s.raised, border: s.border, series: slots[scheme] };
}

/** slot is a series' fixed color; series past the six slots are muted. */
function slot(t: Theme, i: number): string {
  return t.series[i] ?? t.muted;
}

export function escape(text: string): string {
  return text.replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

type Option = echarts.EChartsCoreOption;

const axisLabel = (t: Theme) => ({ color: t.muted, fontFamily: fonts.mono, fontSize: typeScale.micro });

function base(t: Theme, panel: Panel): Option {
  return {
    animation: false,
    textStyle: { color: t.text, fontFamily: fonts.sans, fontSize: typeScale.small },
    aria: { enabled: true, label: { description: panel.description ? `${panel.title}. ${panel.description}` : panel.title } },
    grid: { left: 4, right: 12, top: 8, bottom: 4, containLabel: true },
    tooltip: { trigger: "axis", confine: true, backgroundColor: t.surface, borderColor: t.border, textStyle: { color: t.text, fontSize: typeScale.small } },
  };
}

/** bar draws a grouped frame [category, value] as horizontal bars, largest first. */
export function bar(frame: Frame, panel: Panel, t: Theme): Option {
  const unit = frame.columns[1]?.unit;
  const data = frame.rows.map(r => ({ name: String(r[0] ?? "–") || "–", value: Number(r[1] ?? 0) })).reverse();
  return {
    ...base(t, panel),
    tooltip: { ...base(t, panel).tooltip as object, trigger: "item", formatter: (p: { name: string; value: number }) => `${escape(p.name)}: ${escape(value(p.value, unit))}` },
    xAxis: { type: "value", axisLabel: { ...axisLabel(t), formatter: (n: number) => value(n, unit) }, axisLine: { show: false }, axisTick: { show: false }, splitLine: { lineStyle: { color: t.grid } } },
    yAxis: { type: "category", data: data.map(d => d.name), axisLabel: { color: t.text, fontSize: typeScale.small, width: 140, overflow: "truncate" }, axisLine: { show: false }, axisTick: { show: false } },
    series: [{ type: "bar", data: data.map(d => d.value), itemStyle: { color: slot(t, 0), borderRadius: [0, 2, 2, 0] }, barMaxWidth: 14 }],
  };
}

/** timeseries draws [ts, series, value] rows as one line per series. */
export function timeseries(frame: Frame, panel: Panel, t: Theme): Option {
  const unit = frame.columns[2]?.unit;
  const bySeries = new Map<string, [number, number][]>();
  for (const r of frame.rows) {
    const ts = Date.parse(String(r[0]));
    const n = typeof r[2] === "number" ? r[2] : null;
    if (Number.isNaN(ts) || n === null) continue;
    const key = String(r[1] ?? "");
    if (!bySeries.has(key)) bySeries.set(key, []);
    bySeries.get(key)!.push([ts, n]);
  }
  const series = [...bySeries.entries()].map(([name, data], i) => ({
    type: "line", name: name || label(panel.measure ?? "value"), data, showSymbol: false, sampling: "lttb",
    lineStyle: { width: 1.5, color: slot(t, i) }, itemStyle: { color: slot(t, i) },
    areaStyle: bySeries.size === 1 ? { color: slot(t, 0), opacity: 0.1 } : undefined,
    endLabel: bySeries.size > 1 ? { show: true, color: slot(t, i), fontSize: typeScale.micro, formatter: escape(name) } : undefined,
  }));
  return {
    ...base(t, panel),
    grid: { ...base(t, panel).grid as object, right: bySeries.size > 1 ? 72 : 12 },
    tooltip: {
      ...base(t, panel).tooltip as object,
      axisPointer: { type: "line", lineStyle: { color: t.border } },
      formatter: (items: { seriesName: string; value: [number, number] }[]) => {
        if (!items.length) return "";
        const at = new Date(items[0].value[0]).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
        return [escape(at), ...items.map(i => `${escape(i.seriesName)}: ${escape(value(i.value[1], unit))}`)].join("<br>");
      },
    },
    xAxis: { type: "time", axisLabel: { ...axisLabel(t), hideOverlap: true }, axisLine: { show: false }, axisTick: { show: false }, splitLine: { show: false } },
    yAxis: { type: "value", axisLabel: { ...axisLabel(t), formatter: (n: number) => value(n, unit) }, axisLine: { show: false }, axisTick: { show: false }, splitLine: { lineStyle: { color: t.grid } } },
    dataZoom: [{ type: "inside", filterMode: "none" }],
    series,
  };
}

/** sparkline draws a stat's trend: no axes or labels, the last point marked. */
export function sparkline(trend: (number | null)[], title: string, t: Theme, color?: string): Option {
  const line = color ?? slot(t, 0);
  const points = trend.map((n, i) => [i, n]);
  return {
    animation: false,
    aria: { enabled: true, label: { description: `${title} over the last hour` } },
    grid: { left: 1, right: 3, top: 3, bottom: 3 },
    xAxis: { type: "value", show: false, min: 0, max: Math.max(1, trend.length - 1) },
    yAxis: { type: "value", show: false, scale: true },
    series: [{ type: "line", data: points, showSymbol: false, connectNulls: false, lineStyle: { width: 1.25, color: line }, areaStyle: { color: line, opacity: 0.12 }, silent: true }],
  };
}
