// ECharts option builders for the widgets: the annotated time series of the
// chart card, the heatmap and the session swimlanes. They draw through the
// app's one chart engine (panels/charts.ts) and share its look: mono axes at
// 11 px, no axis lines or ticks, split lines only on the value axis,
// tooltips on the inset surface, no animation.
import { alpha } from "@mantine/core";
import type { CustomSeriesRenderItemAPI, CustomSeriesRenderItemParams } from "echarts";
import { axisLabel, byteSteps, echarts, escape, slot, tooltip, tooltipRow, type Option, type Point, type Theme } from "../panels/charts";
import { duration } from "../panels/format";
import { fonts, typeScale } from "../theme/tokens";
import type { State } from "./types";

export type { Point } from "../panels/charts";

export type Series = { name: string; points: Point[]; /** A categorical slot, the accent, or a state when the line means one. */ color?: "brand" | State | number };

/** A period a session waited on the person, shaded on the chart. */
export type Wait = { from: number; to: number; label: string };

/** A moment worth a mark: a kill, a reclaim, a restart. */
export type Event = { at: number; label: string };

export type Formatter = (n: number) => string;

const bareAxis = { axisLine: { show: false }, axisTick: { show: false } };

const clock = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

function seriesColor(s: Series, i: number, t: Theme): string {
  if (typeof s.color === "number") return slot(t, s.color);
  if (s.color === "brand") return t.brand;
  if (s.color === undefined) return i === 0 ? t.brand : slot(t, i);
  return t.states[s.color];
}

/** timeSeries is the chart card's body: lines with a faded area, the peak
 * as a dashed line labelled at its start, waits as shaded bands labelled
 * along their bottom, and events as labelled marks above the line, so the
 * three kinds of label never share an edge. With several series the legend
 * sits under the axis, in room the grid leaves for it. */
export function timeSeries(input: { series: Series[]; from: number; to: number; unit?: string; format: Formatter; waits?: Wait[]; events?: Event[]; peak?: boolean; label: string }, t: Theme): Option {
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
  return {
    animation: false,
    textStyle: { fontFamily: fonts.sans, color: t.text, fontSize: typeScale.small },
    aria: { enabled: true, label: { description: input.label } },
    grid: { left: 8, right: 12, top: 20, bottom: several ? 36 : 6 },
    legend: several ? { bottom: 0, left: 0, icon: "circle", itemWidth: 8, itemHeight: 8, itemGap: 12, textStyle: { color: t.muted, fontSize: typeScale.small } } : undefined,
    tooltip: tooltip(t, {
      trigger: "axis", axisPointer: { type: "line", lineStyle: { color: t.border } },
      formatter: (items: { seriesName: string; color: string; value: Point }[]) => {
        if (!items.length) return "";
        return [`<div style="color:${t.muted};font-family:${fonts.mono};font-size:${typeScale.micro}px;margin-bottom:4px">${escape(clock(items[0].value[0]))}</div>`, ...items.map(i => tooltipRow(i.color, i.seriesName, format(i.value[1])))].join("");
      },
    }),
    xAxis: { type: "time", min: from, max: to, ...bareAxis, axisLabel: { ...axisLabel(t), hideOverlap: true, formatter: (v: number) => clock(v) }, splitLine: { show: false } },
    yAxis: { type: "value", ...byteSteps(input.unit, max), ...bareAxis, axisLabel: { ...axisLabel(t), formatter: (n: number) => format(n) }, splitLine: { lineStyle: { color: t.grid } } },
    series: series.map((s, i) => {
      const color = seriesColor(s, i, t);
      return {
        type: "line", name: s.name, data: s.points, showSymbol: false, smooth: false, sampling: "lttb",
        lineStyle: { width: 1.5, color }, itemStyle: { color },
        areaStyle: i === 0 ? { color: { type: "linear", x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: alpha(color, 0.28) }, { offset: 1, color: alpha(color, 0) }] } } : undefined,
        emphasis: { disabled: true },
        markLine: i === 0 && peak && s.points.length ? {
          silent: true, symbol: "none", lineStyle: { color: t.muted, type: "dashed", width: 1 },
          label: { formatter: `peak ${format(max)}`, position: "insideStartTop", fontFamily: fonts.mono, fontSize: typeScale.micro, color: t.muted },
          data: [{ type: "max", name: "peak" }],
        } : undefined,
        markArea: i === 0 && waits.length ? {
          silent: true, itemStyle: { color: alpha(t.states.waiting, 0.1) },
          label: { show: true, position: "insideBottom", fontFamily: fonts.mono, fontSize: typeScale.micro, color: t.muted },
          data: waits.map(w => [{ name: w.label, xAxis: w.from }, { xAxis: w.to }]),
        } : undefined,
        markPoint: i === 0 && events.length ? {
          symbol: "circle", symbolSize: 8, itemStyle: { color: t.brand, borderColor: t.surface, borderWidth: 2 },
          label: { show: true, position: "top", formatter: "{b}", fontFamily: fonts.mono, fontSize: typeScale.micro, color: t.text, backgroundColor: t.inset, borderColor: t.border, borderWidth: 1, padding: [2, 5], borderRadius: 3 },
          data: events.map(e => ({ name: e.label, coord: [e.at, valueAt(e.at)] })),
        } : undefined,
      };
    }),
  };
}

/** heatmap is rows × columns of counts in a single-hue ramp of the accent:
 * the darker the cell, the busier the hour. */
export function heatmap(input: { rows: string[]; cols: string[]; values: number[][]; format: Formatter; label: string; unit: string }, t: Theme): Option {
  const { rows, cols, values, format, unit } = input;
  const data = values.flatMap((r, y) => r.map((v, x) => [x, y, v]));
  const max = Math.max(1, ...data.map(d => d[2]));
  return {
    animation: false,
    aria: { enabled: true, label: { description: input.label } },
    tooltip: tooltip(t, { trigger: "item", formatter: (p: { value: [number, number, number] }) => `<span style="font-family:${fonts.mono}">${escape(rows[p.value[1]])} · ${escape(cols[p.value[0]])}</span><br>${escape(format(p.value[2]))} ${escape(unit)}` }),
    grid: { left: 4, right: 4, top: 4, bottom: 4 },
    xAxis: { type: "category", data: cols, ...bareAxis, axisLabel: { ...axisLabel(t), interval: 2 }, splitLine: { show: false } },
    yAxis: { type: "category", data: rows, inverse: true, ...bareAxis, axisLabel: { ...axisLabel(t), fontSize: typeScale.small, color: t.text }, splitLine: { show: false } },
    visualMap: { show: false, min: 0, max, inRange: { color: [alpha(t.brand, 0.08), alpha(t.brand, 0.45), t.brand] } },
    series: [{ type: "heatmap", data, itemStyle: { borderColor: t.surface, borderWidth: 2, borderRadius: 2 }, emphasis: { itemStyle: { borderColor: t.text } } }],
  };
}

export type Lane = { id: string; name: string; segments: { from: number; to: number; state: State }[] };

/** swimlanes is one lane per session on a shared time axis, each lane the
 * segments of what the session was doing, coloured by state, and a line at
 * now. Equal neighbouring states should be merged by the caller. The axis
 * keeps its last ticks clear of the "now" label. */
export function swimlanes(input: { lanes: Lane[]; from: number; to: number; now: number; label: string }, t: Theme): Option {
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
  // A tick within a label's width of now would run into the "now" label.
  const nearNow = (v: number) => to - v < (to - from) * 0.06;
  return {
    animation: false,
    aria: { enabled: true, label: { description: input.label } },
    tooltip: tooltip(t, { trigger: "item", formatter: (p: { name: string; value: [number, number, number, State]; color: string }) => tooltipRow(p.color, p.name, `${p.value[3]} · ${duration(p.value[2] - p.value[1])}`) }),
    grid: { left: 4, right: 36, top: 6, bottom: 4 },
    xAxis: { type: "time", min: from, max: to, ...bareAxis, axisLabel: { ...axisLabel(t), hideOverlap: true, showMaxLabel: false, formatter: (v: number) => nearNow(v) ? "" : clock(v) }, splitLine: { lineStyle: { color: t.grid } } },
    yAxis: { type: "category", data: lanes.map(l => l.name), inverse: true, ...bareAxis, axisLabel: { ...axisLabel(t), fontSize: typeScale.small, color: t.text, width: 150, overflow: "truncate" }, splitLine: { show: false } },
    series: [{
      type: "custom", renderItem: render, encode: { x: [1, 2], y: 0 }, data,
      markLine: { silent: true, symbol: "none", lineStyle: { color: t.text, width: 1 }, label: { formatter: "now", position: "end", fontFamily: fonts.mono, fontSize: typeScale.micro, color: t.muted }, data: [{ xAxis: now }] },
    }],
  };
}
