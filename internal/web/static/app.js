"use strict";
(() => {
  const plots = new Map();
  const theme = matchMedia("(prefers-color-scheme: dark)");
  const css = name => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  function draw() {
    for (const [el, plot] of plots) {
      plot.observer.disconnect();
      plot.chart.destroy();
      plots.delete(el);
    }
    if (!window.uPlot) return;
    document.querySelectorAll(".chart").forEach(el => {
      const buckets = JSON.parse(el.dataset.buckets);
      const load = el.dataset.series === "load";
      if (!buckets[0].length) return;
      const data = [buckets[0], buckets[load ? 2 : 1]];
      const chart = new uPlot({
        width: Math.max(1, el.clientWidth), height: 120,
        padding: [8, 8, 0, 4],
        legend: { show: false },
        cursor: { show: false },
        select: { show: false },
        scales: { x: { time: true }, y: { range: (_, min, max) => [0, Math.max(1, max)] } },
        axes: [
          { stroke: css("--muted"), grid: { show: false }, ticks: { show: false }, size: 28, font: "11px system-ui", space: 90, values: (_, values) => values.map(v => new Date(v * 1000).toLocaleTimeString(undefined, {hour: "numeric", minute: "2-digit"})) },
          { stroke: css("--muted"), grid: { stroke: css("--line"), width: 1 }, ticks: { show: false }, size: 36, font: "11px system-ui" }
        ],
        series: [{}, { label: load ? "Load" : "Events / minute", stroke: css(load ? "--idle" : "--working"), width: 2, spanGaps: false, points: { show: false } }]
      }, data, el);
      const observer = new ResizeObserver(() => {
        if (el.isConnected) chart.setSize({ width: Math.max(1, el.clientWidth), height: 120 });
      });
      observer.observe(el);
      plots.set(el, { chart, observer });
    });
  }
  document.addEventListener("DOMContentLoaded", draw);
  document.addEventListener("htmx:afterSwap", draw);
  theme.addEventListener("change", draw);
  function connection(connected) {
    const label = document.querySelector(".connection");
    if (label) {
      label.textContent = connected ? "Live connection" : "Reconnecting…";
      label.classList.toggle("disconnected", !connected);
    }
  }
  document.addEventListener("htmx:sseOpen", () => connection(true));
  document.addEventListener("htmx:sseError", () => connection(false));
})();
