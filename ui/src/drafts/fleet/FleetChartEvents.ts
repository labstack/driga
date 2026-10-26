// A click on the chart the app's Chart wrapper draws. The wrapper exposes no
// events yet, so this finds its ECharts instance by the element it rendered;
// on promotion, Chart.tsx grows an onClick prop and this file goes.
import { useEffect, type RefObject } from "react";
import { echarts } from "../../panels/charts";

export type ChartClick = { dataType?: string; name?: string; data?: { target?: string } };

/** useChartClick calls onClick for clicks on nodes and links of the chart
 * inside ref, while mounted is true. */
export function useChartClick(ref: RefObject<HTMLElement | null>, onClick: (params: ChartClick) => void, mounted = true) {
  useEffect(() => {
    if (!mounted) return;
    const el = ref.current?.querySelector<HTMLElement>('[role="img"]');
    const chart = el ? echarts.getInstanceByDom(el) : undefined;
    if (!chart) return;
    const handler = (params: unknown) => onClick(params as ChartClick);
    chart.on("click", handler);
    return () => { chart.off("click", handler); };
  }, [ref, onClick, mounted]);
}
