// ECharts option builders for the widget drafts. They extend the app's one
// chart engine (panels/charts.ts) with the series and components the drafts
// need and share one look: mono axes at 11 px, no axis lines or ticks, split
// lines only on the value axis, tooltips on the inset surface, no animation.
import type { CustomSeriesRenderItemAPI, CustomSeriesRenderItemParams } from "echarts";
import { CustomChart, HeatmapChart, PieChart } from "echarts/charts";
import { GraphicComponent, LegendComponent, MarkAreaComponent, MarkLineComponent, VisualMapComponent } from "echarts/components";
import { byteSteps, echarts, escape } from "../../panels/charts";
import { duration } from "../../panels/format";
import { fade, type WidgetTheme } from "./palette";
import type { State } from "./types";

echarts.use([PieChart, HeatmapChart, CustomChart, MarkLineComponent, MarkAreaComponent, LegendComponent, VisualMapComponent, GraphicComponent]);

export type Option = echarts.EChartsCoreOption;

/** A time series' points as [epoch ms, value]. */
export type Point = [number, number];

export type Series = { name: string; points: Point[]; color?: "brand" | State | number };

/** A period a session waited on the person, shaded on the chart. */
export type Wait = { from: number; to: number; label: string };

/** A moment worth a mark: a kill, a reclaim, a restart. */
export type Event = { at: number; label: string };

export type Formatter = (n: number) => string;

const axisLabel = (t: WidgetTheme) => ({ color: t.muted, fontFamily: t.mono, fontSize: t.micro });

const bareAxis = { axisLine: { show: false }, axisTick: { show: false } };

function tooltip(t: WidgetTheme, extra: object = {}) {
  return { confine: true, backgroundColor: t.inset, borderColor: t.border, borderWidth: 1, borderRadius: 6, padding: [6, 10], textStyle: { color: t.text, fontSize: t.small, fontFamily: t.sans }, extraCssText: "box-shadow: none;", ...extra };
}

/** A tooltip row: dot, name, value right-aligned in mono. */
function row(color: string, name: string, value: string, t: WidgetTheme): string {
  return `<div style="display:flex;align-items:center;gap:8px;justify-content:space-between"><span style="display:flex;align-items:center;gap:6px"><span style="width:8px;height:8px;border-radius:50%;background:${color}"></span>${escape(name)}</span><span style="font-family:${t.mono};font-variant-numeric:tabular-nums">${escape(value)}</span></div>`;
}

function seriesColor(s: Series, i: number, t: WidgetTheme): string {
  if (typeof s.color === "number") return t.palette[s.color] ?? t.muted;
  if (s.color === "brand" || s.color === undefined) return i === 0 ? t.brand : t.palette[i] ?? t.muted;
  return t.states[s.color];
}

/** sparkline is a trend with no axes: a 1.5 px line, the area fading to the
 * baseline, the last reading marked. Unsmoothed, so spikes stay spikes. */
export function sparkline(trend: (number | null)[], color: string, label: string): Option {
  const points = trend.map((n, i) => [i, n]);
  let last: (number | null)[] | undefined;
  for (let i = points.length - 1; i >= 0 && !last; i--) if (points[i][1] !== null) last = points[i];
  return {
    animation: false, silent: true,
    aria: { enabled: true, label: { description: label } },
    grid: { left: 1, right: 3, top: 3, bottom: 3 },
    xAxis: { type: "value", show: false, min: 0, max: Math.max(1, trend.length - 1) },
    yAxis: { type: "value", show: false, scale: true },
    series: [{
      type: "line", data: points, showSymbol: false, connectNulls: false, smooth: false, silent: true,
      lineStyle: { width: 1.5, color },
      areaStyle: { color: { type: "linear", x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: fade(color, 0.24) }, { offset: 1, color: fade(color, 0) }] } },
      markPoint: last ? { symbol: "circle", symbolSize: 4, silent: true, label: { show: false }, itemStyle: { color }, data: [{ coord: last }] } : undefined,
    }],
  };
}

/** timeSeries is the chart card's body: lines with a faded area, the peak
 * as a dashed line, waits as shaded bands and events as labelled marks. */
export function timeSeries(input: { series: Series[]; from: number; to: number; unit?: string; format: Formatter; waits?: Wait[]; events?: Event[]; peak?: boolean; label: string }, t: WidgetTheme): Option {
  const { series, from, to, format, waits = [], events = [], peak = true } = input;
  const several = series.length > 1;
  const max = Math.max(0, ...series.flatMap(s => s.points.map(p => p[1])));
  const primary = series[0];
  const valueAt = (at: number): number => {
    if (!primary?.points.length) return 0;
    let best = primary.points[0];
    for (const p of primary.points) if (Math.abs(p[0] - at) < Math.abs(best[0] - at)) best = p;
    return best[1];
  };
  const clock = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return {
    animation: false,
    textStyle: { fontFamily: t.sans, color: t.text, fontSize: t.small },
    aria: { enabled: true, label: { description: input.label } },
    grid: { left: 8, right: 12, top: 20, bottom: several ? 28 : 6 },
    legend: several ? { bottom: 0, left: 0, icon: "circle", itemWidth: 8, itemHeight: 8, itemGap: 12, textStyle: { color: t.muted, fontSize: t.small } } : undefined,
    tooltip: tooltip(t, {
      trigger: "axis", axisPointer: { type: "line", lineStyle: { color: t.border } },
      formatter: (items: { seriesName: string; color: string; value: Point }[]) => {
        if (!items.length) return "";
        return [`<div style="color:${t.muted};font-family:${t.mono};font-size:${t.micro}px;margin-bottom:4px">${escape(clock(items[0].value[0]))}</div>`, ...items.map(i => row(i.color, i.seriesName, format(i.value[1]), t))].join("");
      },
    }),
    xAxis: { type: "time", min: from, max: to, ...bareAxis, axisLabel: { ...axisLabel(t), hideOverlap: true, formatter: (v: number) => clock(v) }, splitLine: { show: false } },
    yAxis: { type: "value", ...byteSteps(input.unit, max), ...bareAxis, axisLabel: { ...axisLabel(t), formatter: (n: number) => format(n) }, splitLine: { lineStyle: { color: t.grid } } },
    series: series.map((s, i) => {
      const color = seriesColor(s, i, t);
      return {
        type: "line", name: s.name, data: s.points, showSymbol: false, smooth: false, sampling: "lttb",
        lineStyle: { width: 1.5, color }, itemStyle: { color },
        areaStyle: i === 0 ? { color: { type: "linear", x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: fade(color, 0.28) }, { offset: 1, color: fade(color, 0) }] } } : undefined,
        emphasis: { disabled: true },
        markLine: i === 0 && peak && s.points.length ? {
          silent: true, symbol: "none", lineStyle: { color: t.muted, type: "dashed", width: 1 },
          label: { formatter: `peak ${format(max)}`, position: "insideEndTop", fontFamily: t.mono, fontSize: t.micro, color: t.muted },
          data: [{ type: "max", name: "peak" }],
        } : undefined,
        markArea: i === 0 && waits.length ? {
          silent: true, itemStyle: { color: fade(t.states.waiting, 0.1) },
          label: { show: true, position: "insideTop", fontFamily: t.mono, fontSize: t.micro, color: t.muted },
          data: waits.map(w => [{ name: w.label, xAxis: w.from }, { xAxis: w.to }]),
        } : undefined,
        markPoint: i === 0 && events.length ? {
          symbol: "circle", symbolSize: 8, itemStyle: { color: t.brand, borderColor: t.surface, borderWidth: 2 },
          label: { show: true, position: "top", formatter: "{b}", fontFamily: t.mono, fontSize: t.micro, color: t.text, backgroundColor: t.inset, borderColor: t.border, borderWidth: 1, padding: [2, 5], borderRadius: 3 },
          data: events.map(e => ({ name: e.label, coord: [e.at, valueAt(e.at)] })),
        } : undefined,
      };
    }),
  };
}

/** donut is a share of a total: slices in the categorical slots, no labels
 * on the ring (the legend list beside it carries them), the centre free for
 * the total. */
export function donut(slices: { name: string; value: number; color?: number }[], format: Formatter, label: string, t: WidgetTheme): Option {
  const total = slices.reduce((s, x) => s + x.value, 0);
  return {
    animation: false,
    aria: { enabled: true, label: { description: label } },
    tooltip: tooltip(t, { trigger: "item", formatter: (p: { name: string; value: number; color: string; percent: number }) => row(p.color, p.name, `${format(p.value)} · ${Math.round(p.percent)}%`, t) }),
    series: [{
      type: "pie", radius: ["64%", "84%"], center: ["50%", "50%"], avoidLabelOverlap: false,
      label: { show: false }, labelLine: { show: false },
      itemStyle: { borderColor: t.surface, borderWidth: 2, borderRadius: 2 },
      emphasis: { scale: false, itemStyle: { borderColor: t.text } },
      data: slices.map((s, i) => ({ name: s.name, value: s.value, itemStyle: { color: t.palette[s.color ?? i] ?? t.muted } })),
    }],
    graphic: total === 0 ? [{ type: "text", left: "center", top: "middle", style: { text: "no data", fill: t.muted, font: `${t.small}px ${t.sans}` } }] : [],
  };
}

/** heatmap is rows × columns of counts in a single-hue ramp of the accent:
 * the darker the cell, the busier the hour. */
export function heatmap(input: { rows: string[]; cols: string[]; values: number[][]; format: Formatter; label: string; unit: string }, t: WidgetTheme): Option {
  const { rows, cols, values, format, unit } = input;
  const data = values.flatMap((r, y) => r.map((v, x) => [x, y, v]));
  const max = Math.max(1, ...data.map(d => d[2]));
  return {
    animation: false,
    aria: { enabled: true, label: { description: input.label } },
    tooltip: tooltip(t, { trigger: "item", formatter: (p: { value: [number, number, number] }) => `<span style="font-family:${t.mono}">${escape(rows[p.value[1]])} · ${escape(cols[p.value[0]])}</span><br>${escape(format(p.value[2]))} ${escape(unit)}` }),
    grid: { left: 4, right: 4, top: 4, bottom: 4 },
    xAxis: { type: "category", data: cols, ...bareAxis, axisLabel: { ...axisLabel(t), interval: 2 }, splitLine: { show: false } },
    yAxis: { type: "category", data: rows, inverse: true, ...bareAxis, axisLabel: { ...axisLabel(t), fontSize: t.small, color: t.text }, splitLine: { show: false } },
    visualMap: { show: false, min: 0, max, inRange: { color: [fade(t.brand, 0.08), fade(t.brand, 0.45), t.brand] } },
    series: [{ type: "heatmap", data, itemStyle: { borderColor: t.surface, borderWidth: 2, borderRadius: 2 }, emphasis: { itemStyle: { borderColor: t.text } } }],
  };
}

export type Lane = { id: string; name: string; segments: { from: number; to: number; state: State }[] };

/** swimlanes is one lane per session on a shared time axis, each lane the
 * segments of what the session was doing, coloured by state, and a line at
 * now. Equal neighbouring states should be merged by the caller. */
export function swimlanes(input: { lanes: Lane[]; from: number; to: number; now: number; label: string }, t: WidgetTheme): Option {
  const { lanes, from, to, now } = input;
  const data = lanes.flatMap((l, y) => l.segments.map(s => ({ value: [y, s.from, s.to, s.state], name: l.name, itemStyle: { color: t.states[s.state] } })));
  const render = (params: CustomSeriesRenderItemParams, api: CustomSeriesRenderItemAPI) => {
    const lane = api.value(0) as number;
    const start = api.coord([api.value(1), lane]);
    const end = api.coord([api.value(2), lane]);
    const height = (api.size!([0, 1]) as number[])[1] * 0.5;
    const sys = params.coordSys as unknown as { x: number; y: number; width: number; height: number };
    const rect = echarts.graphic.clipRectByRect({ x: start[0], y: start[1] - height / 2, width: Math.max(2, end[0] - start[0]), height }, sys);
    return rect ? { type: "rect", shape: { ...rect, r: 2 }, style: { fill: t.states[api.value(3) as State] }, transition: [] } : null;
  };
  return {
    animation: false,
    aria: { enabled: true, label: { description: input.label } },
    tooltip: tooltip(t, { trigger: "item", formatter: (p: { name: string; value: [number, number, number, State]; color: string }) => row(p.color, p.name, `${p.value[3]} · ${duration(p.value[2] - p.value[1])}`, t) }),
    grid: { left: 4, right: 36, top: 6, bottom: 4 },
    xAxis: { type: "time", min: from, max: to, ...bareAxis, axisLabel: { ...axisLabel(t), hideOverlap: true, formatter: (v: number) => new Date(v).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) }, splitLine: { lineStyle: { color: t.grid } } },
    yAxis: { type: "category", data: lanes.map(l => l.name), inverse: true, ...bareAxis, axisLabel: { ...axisLabel(t), fontSize: t.small, color: t.text, width: 150, overflow: "truncate" }, splitLine: { show: false } },
    series: [{
      type: "custom", renderItem: render, encode: { x: [1, 2], y: 0 }, data,
      markLine: { silent: true, symbol: "none", lineStyle: { color: t.text, width: 1 }, label: { formatter: "now", position: "end", fontFamily: t.mono, fontSize: t.micro, color: t.muted }, data: [{ xAxis: now }] },
    }],
  };
}
