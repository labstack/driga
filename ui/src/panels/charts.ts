// Compiles Driga panels to ECharts options. The model writes Driga's panel
// spec, never ECharts options: formatters, colors and layout stay here, and
// every string from the data is escaped before it reaches a tooltip. This is
// the one place ECharts is assembled: every chart kind and component the app
// draws registers here, and every option builder reads the same Theme.
import { BarChart, CustomChart, HeatmapChart, LineChart, SankeyChart } from "echarts/charts";
import { AriaComponent, DataZoomComponent, GridComponent, LegendComponent, MarkAreaComponent, MarkLineComponent, MarkPointComponent, TooltipComponent, VisualMapComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer, SVGRenderer } from "echarts/renderers";
import { alpha } from "@mantine/core";
import { fonts, series as slots, surfaces, tones, typeScale, type Scheme } from "../theme/tokens";
import type { State } from "../widgets/types";
import { label, value } from "./format";
import type { Frame, Panel } from "./types";

echarts.use([BarChart, LineChart, SankeyChart, HeatmapChart, CustomChart, MarkPointComponent, MarkLineComponent, MarkAreaComponent, GridComponent, TooltipComponent, DataZoomComponent, LegendComponent, VisualMapComponent, AriaComponent, CanvasRenderer, SVGRenderer]);

export { echarts };

/** Charts draw into a canvas that cannot read CSS variables, so they take
 * resolved colors from the same tokens the theme uses, per scheme: the text
 * and surface roles, the accent, the six series slots and the four state
 * colors (finished is the quiet line color, since it needs nothing). */
export type Theme = {
  scheme: Scheme;
  text: string; muted: string; line: string; grid: string;
  surface: string; inset: string; border: string;
  brand: string; series: readonly string[]; states: Record<State, string>;
};

export function chartTheme(scheme: Scheme): Theme {
  const s = surfaces[scheme];
  const t = tones[scheme];
  return {
    scheme, text: s.text, muted: s.muted, line: s.controlBorder, grid: s.borderSoft, surface: s.raised, inset: s.inset, border: s.border,
    brand: t.brand, series: slots[scheme], states: { working: t.success, waiting: t.warning, stuck: t.danger, idle: t.info, finished: s.controlBorder },
  };
}

/** slot is a series' fixed color; series past the six slots are muted. */
export function slot(t: Theme, i: number): string {
  return t.series[i] ?? t.muted;
}

export function escape(text: string): string {
  return text.replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

export type Option = echarts.EChartsCoreOption;

export const axisLabel = (t: Theme) => ({ color: t.muted, fontFamily: fonts.mono, fontSize: typeScale.micro });

/** tooltip is the shared tooltip box: on the inset surface, confined to
 * the chart, in the small size. */
export function tooltip(t: Theme, extra: object = {}): object {
  return { confine: true, backgroundColor: t.inset, borderColor: t.border, borderWidth: 1, borderRadius: 6, padding: [6, 10], textStyle: { color: t.text, fontSize: typeScale.small, fontFamily: fonts.sans }, extraCssText: "box-shadow: none;", ...extra };
}

/** A tooltip row: dot, name, value right-aligned in mono. */
export function tooltipRow(color: string, name: string, text: string): string {
  return `<div style="display:flex;align-items:center;gap:8px;justify-content:space-between"><span style="display:flex;align-items:center;gap:6px"><span style="width:8px;height:8px;border-radius:50%;background:${color}"></span>${escape(name)}</span><span style="font-family:${fonts.mono};font-variant-numeric:tabular-nums">${escape(text)}</span></div>`;
}

function base(t: Theme, panel: Panel): Option {
  return {
    animation: false,
    textStyle: { color: t.text, fontFamily: fonts.sans, fontSize: typeScale.small },
    aria: { enabled: true, label: { description: panel.description ? `${panel.title}. ${panel.description}` : panel.title } },
    // ECharts 6 keeps axis labels inside the grid's bounds by itself.
    grid: { left: 4, right: 12, top: 8, bottom: 4 },
    tooltip: tooltip(t, { trigger: "axis" }),
  };
}

/** bar draws a grouped frame [category, value] as horizontal bars, largest first. */
export function bar(frame: Frame, panel: Panel, t: Theme): Option {
  const unit = frame.columns[1]?.unit;
  const data = frame.rows.map(r => ({ name: String(r[0] ?? "–") || "–", value: Number(r[1] ?? 0) })).reverse();
  return {
    ...base(t, panel),
    tooltip: tooltip(t, { trigger: "item", formatter: (p: { name: string; value: number }) => `${escape(p.name)}: ${escape(value(p.value, unit))}` }),
    xAxis: { type: "value", ...byteSteps(unit, Math.max(0, ...data.map(d => d.value))), axisLabel: { ...axisLabel(t), formatter: (n: number) => value(n, unit) }, axisLine: { show: false }, axisTick: { show: false }, splitLine: { lineStyle: { color: t.grid } } },
    yAxis: { type: "category", data: data.map(d => d.name), axisLabel: { color: t.text, fontSize: typeScale.small, width: 140, overflow: "truncate", interval: 0 }, axisLine: { show: false }, axisTick: { show: false } },
    series: [{ type: "bar", data: data.map(d => d.value), itemStyle: { color: slot(t, 0), borderRadius: [0, 2, 2, 0] }, barMaxWidth: 14 }],
  };
}

/** summary says in words what a chart shows, as Canon's chart card asks:
 * the largest bar and the total, or a series' latest reading and its peak.
 * Several series name the highest now. */
export function summary(frame: Frame, panel: Panel): string {
  if (panel.viz === "bar") {
    const unit = frame.columns[1]?.unit;
    const data = frame.rows.map(r => ({ name: String(r[0] ?? "–") || "–", n: Number(r[1] ?? 0) }));
    if (!data.length) return "";
    const top = data.reduce((a, b) => (b.n > a.n ? b : a));
    const total = data.reduce((sum, d) => sum + d.n, 0);
    return `${top.name} most, ${value(top.n, unit)} of ${value(total, unit)}`;
  }
  if (panel.viz !== "timeseries") return "";
  const unit = frame.columns[2]?.unit;
  const last = new Map<string, number>();
  let peak = -Infinity;
  for (const r of frame.rows) {
    if (typeof r[2] !== "number") continue;
    last.set(String(r[1] ?? ""), r[2]);
    peak = Math.max(peak, r[2]);
  }
  if (!last.size) return "";
  if (last.size === 1) return `${value([...last.values()][0], unit)} now, peak ${value(peak, unit)}`;
  const [name, n] = [...last.entries()].reduce((a, b) => (b[1] > a[1] ? b : a));
  return `${name} highest now at ${value(n, unit)}`;
}

/** A time series' points as [epoch ms, value]. */
export type Point = [number, number];

/** timeseriesOf reads [ts, series, value] rows into one series per name,
 * in the order the names first appear. */
export function timeseriesOf(frame: Frame, panel: Panel): { name: string; points: Point[] }[] {
  const bySeries = new Map<string, Point[]>();
  for (const r of frame.rows) {
    const ts = Date.parse(String(r[0]));
    const n = typeof r[2] === "number" ? r[2] : null;
    if (Number.isNaN(ts) || n === null) continue;
    const key = String(r[1] ?? "");
    if (!bySeries.has(key)) bySeries.set(key, []);
    bySeries.get(key)!.push([ts, n]);
  }
  return [...bySeries.entries()].map(([name, points]) => ({ name: name || label(panel.measure ?? "value"), points }));
}

/** timeseries draws [ts, series, value] rows as one line per series. */
export function timeseries(frame: Frame, panel: Panel, t: Theme): Option {
  const unit = frame.columns[2]?.unit;
  const lines = timeseriesOf(frame, panel);
  const series = lines.map(({ name, points }, i) => ({
    type: "line", name, data: points, showSymbol: false, sampling: "lttb",
    lineStyle: { width: 1.5, color: slot(t, i) }, itemStyle: { color: slot(t, i) },
    areaStyle: lines.length === 1 ? { color: slot(t, 0), opacity: 0.1 } : undefined,
    endLabel: lines.length > 1 ? { show: true, color: slot(t, i), fontSize: typeScale.micro, formatter: escape(name) } : undefined,
  }));
  return {
    ...base(t, panel),
    grid: { ...base(t, panel).grid as object, right: lines.length > 1 ? 72 : 12 },
    tooltip: tooltip(t, {
      trigger: "axis",
      axisPointer: { type: "line", lineStyle: { color: t.border } },
      formatter: (items: { seriesName: string; value: [number, number] }[]) => {
        if (!items.length) return "";
        const at = new Date(items[0].value[0]).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
        return [escape(at), ...items.map(i => `${escape(i.seriesName)}: ${escape(value(i.value[1], unit))}`)].join("<br>");
      },
    }),
    xAxis: { type: "time", axisLabel: { ...axisLabel(t), hideOverlap: true }, axisLine: { show: false }, axisTick: { show: false }, splitLine: { show: false } },
    yAxis: { type: "value", ...byteSteps(unit, Math.max(0, ...lines.flatMap(l => l.points).map(p => p[1]))), axisLabel: { ...axisLabel(t), formatter: (n: number) => value(n, unit) }, axisLine: { show: false }, axisTick: { show: false }, splitLine: { lineStyle: { color: t.grid } } },
    dataZoom: [{ type: "inside", filterMode: "none" }],
    series,
  };
}

/** sparkline draws a stat's trend: no axes or labels, the last point
 * marked, in the accent unless the metric has a color of its own. */
export function sparkline(trend: (number | null)[], title: string, t: Theme, color?: string, window = "hour"): Option {
  const line = color ?? t.brand;
  const points = trend.map((n, i) => [i, n]);
  return {
    animation: false,
    aria: { enabled: true, label: { description: `${title} over the last ${window}` } },
    grid: { left: 1, right: 3, top: 3, bottom: 3 },
    xAxis: { type: "value", show: false, min: 0, max: Math.max(1, trend.length - 1) },
    yAxis: { type: "value", show: false, scale: true },
    series: [{
      type: "line", data: points, showSymbol: false, connectNulls: false, smooth: 0.3, silent: true,
      lineStyle: { width: 1.5, color: line },
      // The area fades toward the baseline, so the line carries the shape.
      areaStyle: { color: { type: "linear", x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: alpha(line, 0.32) }, { offset: 1, color: alpha(line, 0) }] } },
      // The latest reading is marked, since it is the value beside the line.
      markPoint: { symbol: "circle", symbolSize: 4, silent: true, label: { show: false }, itemStyle: { color: line }, data: lastPoint(points) },
    }],
  };
}

function lastPoint(points: (number | null)[][]): { coord: (number | null)[] }[] {
  for (let i = points.length - 1; i >= 0; i--) if (points[i][1] !== null) return [{ coord: points[i] }];
  return [];
}

/** A sankey node: its id, column, the label drawn (ECharts rich text, so
 * braces are the caller's to escape) and the tooltip's HTML, already escaped. */
export type SankeyNode = { name: string; depth: number; text: string; tip: string; itemStyle?: object; label?: object };

export type SankeyLink = { source: string; target: string; value: number; tip: string; lineStyle?: object };

/** sankey draws nodes in fixed columns joined by ribbons whose width is
 * their value. Nothing moves: the layout runs no iterations and nodes are
 * not draggable, so the picture is the same after every refresh. Hovering
 * a node lights its whole path. */
export function sankey(input: { nodes: SankeyNode[]; links: SankeyLink[]; description: string; rich?: Record<string, object>; labelWidth?: number; left?: number; right?: number }, t: Theme): Option {
  return {
    animation: false,
    textStyle: { color: t.text, fontFamily: fonts.sans, fontSize: typeScale.small },
    aria: { enabled: true, label: { description: input.description } },
    tooltip: tooltip(t, { trigger: "item", formatter: (p: { data?: { tip?: string } }) => p.data?.tip ?? "" }),
    series: [{
      type: "sankey",
      left: input.left ?? 100, right: input.right ?? 350, top: 8, bottom: 8,
      nodeWidth: 12, nodeGap: 12, nodeAlign: "justify", layoutIterations: 0, draggable: false,
      lineStyle: { color: "gradient", opacity: 0.22, curveness: 0.5 },
      emphasis: { focus: "adjacency", lineStyle: { opacity: 0.5 } },
      label: {
        fontFamily: fonts.mono, fontSize: typeScale.small, color: t.text, width: input.labelWidth ?? 340, overflow: "truncate",
        formatter: (p: { data?: { text?: string } }) => p.data?.text ?? "",
        rich: input.rich,
      },
      data: input.nodes,
      links: input.links,
    }],
  };
}

/** byteSteps puts a byte axis on 1024-based steps, 4 GB rather than 4.7 GB,
 * with four to six of them up to the largest value. */
export function byteSteps(unit: string | undefined, max: number): { interval?: number; max?: number } {
  if (unit !== "bytes" || !(max > 0)) return {};
  for (let power = 1; power <= 1024 ** 5; power *= 1024) {
    for (const k of [1, 2, 4, 8, 16, 32, 64, 128, 256, 512]) {
      const step = k * power;
      if (max / step <= 5) return { interval: step, max: Math.ceil(max / step) * step };
    }
  }
  return {};
}
