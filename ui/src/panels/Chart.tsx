import { useComputedColorScheme } from "@mantine/core";
import { useEffect, useRef } from "react";
import { echarts, type Theme, chartTheme } from "./charts";

/** Chart is Driga's one ECharts wrapper: it creates the instance on mount,
 * replaces the option when the data or color scheme changes, resizes with its
 * box and disposes on unmount, so chat cards that scroll away release it.
 * Small charts render as SVG, the rest to canvas. */
export function Chart({ option, label, height, small }: { option: (t: Theme) => echarts.EChartsCoreOption; label: string; height?: number | string; small?: boolean }) {
  const scheme = useComputedColorScheme("dark");
  const el = useRef<HTMLDivElement>(null);
  const chart = useRef<echarts.ECharts | null>(null);
  useEffect(() => {
    if (!el.current) return;
    const instance = echarts.init(el.current, undefined, { renderer: small ? "svg" : "canvas" });
    chart.current = instance;
    const observer = new ResizeObserver(() => instance.resize());
    observer.observe(el.current);
    return () => { observer.disconnect(); instance.dispose(); chart.current = null; };
  }, [small]);
  useEffect(() => {
    chart.current?.setOption(option(chartTheme(scheme)), { notMerge: true });
  }, [option, scheme]);
  return <div ref={el} role="img" aria-label={label} style={{ height: height ?? "100%", width: "100%", minHeight: small ? 0 : 80 }} />;
}
