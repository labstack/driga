import { useComputedColorScheme } from "@mantine/core";
import { useEffect, useRef } from "react";
import { echarts, type Theme, chartTheme } from "./charts";

/** What a chart event carries: the node or item under the pointer, as
 * ECharts reports it. A drill-down reads `name`, `data` or `value`. */
export type ChartEvent = { dataType?: string; name?: string; data?: unknown; value?: unknown; seriesIndex?: number; dataIndex?: number };

const events = ["click"] as const;

export type ChartEventName = (typeof events)[number];

/** Chart is Driga's one ECharts wrapper: it creates the instance on mount,
 * replaces the option when the data or color scheme changes, resizes with its
 * box and disposes on unmount, so chat cards that scroll away release it.
 * Small charts render as SVG, the rest to canvas. `onEvent` hears the
 * instance's events, so a view never reaches for the instance itself. */
export function Chart({ option, label, height, small, onEvent }: { option: (t: Theme) => echarts.EChartsCoreOption; label: string; height?: number | string; small?: boolean; onEvent?: (name: ChartEventName, params: ChartEvent) => void }) {
  const scheme = useComputedColorScheme("dark");
  const el = useRef<HTMLDivElement>(null);
  const chart = useRef<echarts.ECharts | null>(null);
  // The latest handler, so the instance is wired once and never re-made
  // because a callback was re-created.
  const handler = useRef(onEvent);
  handler.current = onEvent;
  useEffect(() => {
    if (!el.current) return;
    const instance = echarts.init(el.current, undefined, { renderer: small ? "svg" : "canvas" });
    chart.current = instance;
    for (const name of events) instance.on(name, (params: unknown) => handler.current?.(name, params as ChartEvent));
    const observer = new ResizeObserver(() => instance.resize());
    observer.observe(el.current);
    return () => { observer.disconnect(); instance.dispose(); chart.current = null; };
  }, [small]);
  useEffect(() => {
    chart.current?.setOption(option(chartTheme(scheme)), { notMerge: true });
  }, [option, scheme]);
  return <div ref={el} role="img" aria-label={label} style={{ height: height ?? "100%", width: "100%", minHeight: small ? 0 : 80 }} />;
}
