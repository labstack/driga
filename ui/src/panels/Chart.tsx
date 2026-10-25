import { useEffect, useRef } from "react";
import { bar, echarts, theme, timeseries } from "./charts";
import type { Frame, Panel } from "./types";

/** Chart owns one ECharts instance: created on mount, updated in place on
 * refresh, resized with its box and disposed on unmount, so chat cards that
 * scroll away release their canvas. */
export function Chart({ panel, frame, height }: { panel: Panel; frame: Frame; height?: number }) {
  const el = useRef<HTMLDivElement>(null);
  const chart = useRef<echarts.ECharts | null>(null);
  useEffect(() => {
    if (!el.current) return;
    const instance = echarts.init(el.current, undefined, { renderer: "canvas" });
    chart.current = instance;
    const observer = new ResizeObserver(() => instance.resize());
    observer.observe(el.current);
    return () => { observer.disconnect(); instance.dispose(); chart.current = null; };
  }, []);
  useEffect(() => {
    if (!chart.current || !el.current) return;
    const t = theme(el.current);
    chart.current.setOption(panel.viz === "bar" ? bar(frame, panel, t) : timeseries(frame, panel, t), { notMerge: true });
  }, [panel, frame]);
  return <div ref={el} role="img" aria-label={panel.description ? `${panel.title}: ${panel.description}` : panel.title} style={{ height: height ?? "100%", width: "100%", minHeight: 80 }} />;
}
