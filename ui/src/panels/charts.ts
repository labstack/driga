// Compiles Driga panels to ECharts options. The model writes Driga's panel
// spec, never ECharts options: formatters, colors and layout stay here, and
// every string from the data is escaped before it reaches a tooltip.
import { BarChart, LineChart } from "echarts/charts";
import { AriaComponent, DataZoomComponent, GridComponent, TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { label, value } from "./format";
import type { Frame, Panel } from "./types";

echarts.use([BarChart, LineChart, GridComponent, TooltipComponent, DataZoomComponent, AriaComponent, CanvasRenderer]);

export { echarts };

export type Theme = { fg: string; dim: string; line: string; pane: string; series: string[]; font: string };

/** theme reads the console palette from CSS variables, so charts follow the
 * page's light or dark theme. */
export function theme(el: Element): Theme {
  const css = getComputedStyle(el);
  const v = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;
  return {
    fg: v("--fg", "#cdd6f4"), dim: v("--dim", "#a6adc8"), line: v("--line", "#45475a"), pane: v("--pane", "#181825"),
    series: [v("--idle", "#89b4fa"), v("--accent", "#cba6f7"), v("--observation", "#94e2d5"), v("--warn", "#fab387"), v("--working", "#a6e3a1")],
    font: css.fontFamily,
  };
}

export function escape(text: string): string {
  return text.replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

type Option = echarts.EChartsCoreOption;

function base(t: Theme, panel: Panel): Option {
  return {
    animation: false,
    textStyle: { color: t.fg, fontFamily: t.font, fontSize: 12 },
    aria: { enabled: true, label: { description: panel.description ? `${panel.title}. ${panel.description}` : panel.title } },
    grid: { left: 8, right: 16, top: 12, bottom: 8, containLabel: true },
    tooltip: { trigger: "axis", backgroundColor: t.pane, borderColor: t.line, textStyle: { color: t.fg, fontFamily: t.font, fontSize: 12 }, confine: true },
  };
}

/** bar draws a grouped frame [category, value] as horizontal bars, largest first. */
export function bar(frame: Frame, panel: Panel, t: Theme): Option {
  const unit = frame.columns[1]?.unit;
  const data = frame.rows.map(r => ({ name: String(r[0] ?? "–") || "–", value: Number(r[1] ?? 0) })).reverse();
  return {
    ...base(t, panel),
    tooltip: { ...base(t, panel).tooltip as object, trigger: "item", formatter: (p: { name: string; value: number }) => `${escape(p.name)}: ${escape(value(p.value, unit))}` },
    xAxis: { type: "value", axisLabel: { color: t.dim, formatter: (n: number) => value(n, unit) }, splitLine: { lineStyle: { color: t.line, opacity: 0.5 } } },
    yAxis: { type: "category", data: data.map(d => d.name), axisLabel: { color: t.fg, width: 140, overflow: "truncate" }, axisLine: { lineStyle: { color: t.line } }, axisTick: { show: false } },
    // Bars are normal state, so they stay quiet: the palette's blue, softened.
    series: [{ type: "bar", data: data.map(d => d.value), itemStyle: { color: t.series[0], opacity: 0.55, borderRadius: [0, 3, 3, 0] }, emphasis: { itemStyle: { opacity: 0.9 } }, barMaxWidth: 16 }],
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
    lineStyle: { width: 1.5, color: t.series[i % t.series.length] }, itemStyle: { color: t.series[i % t.series.length] },
    areaStyle: bySeries.size === 1 ? { color: t.series[0], opacity: 0.08 } : undefined,
  }));
  return {
    ...base(t, panel),
    tooltip: {
      ...base(t, panel).tooltip as object,
      formatter: (items: { seriesName: string; value: [number, number] }[]) => {
        if (!items.length) return "";
        const at = new Date(items[0].value[0]).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
        return [escape(at), ...items.map(i => `${escape(i.seriesName)}: ${escape(value(i.value[1], unit))}`)].join("<br>");
      },
    },
    xAxis: { type: "time", axisLabel: { color: t.dim, hideOverlap: true }, axisLine: { lineStyle: { color: t.line } }, splitLine: { show: false } },
    yAxis: { type: "value", axisLabel: { color: t.dim, formatter: (n: number) => value(n, unit) }, splitLine: { lineStyle: { color: t.line, opacity: 0.5 } } },
    dataZoom: [{ type: "inside", filterMode: "none" }],
    series,
  };
}
