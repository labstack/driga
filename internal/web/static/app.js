"use strict";
(() => {
  const plots = new Map();
  const system = matchMedia("(prefers-color-scheme: dark)");
  const root = document.documentElement;
  const css = name => getComputedStyle(root).getPropertyValue(name).trim();
  let preference = "system";
  try {
    const saved = localStorage.getItem("driga-theme");
    if (["system", "light", "dark"].includes(saved)) preference = saved;
  } catch (_) { /* Storage can be unavailable in private or restricted contexts. */ }
  function applyTheme() {
    if (preference === "system") root.removeAttribute("data-theme");
    else root.dataset.theme = preference;
    const control = document.getElementById("theme");
    if (control) control.value = preference;
  }
  applyTheme();
  function draw() {
    for (const {observer, chart} of plots.values()) {
      observer.disconnect(); chart.destroy();
    }
    plots.clear();
    document.querySelectorAll(".segment").forEach(el => { el.style.flexGrow = el.value; });
    if (!window.uPlot) return;
    document.querySelectorAll(".chart[data-buckets]").forEach(el => {
      const buckets = JSON.parse(el.dataset.buckets);
      const times = buckets[0];
      if (!times.length) return;
      const end = times[times.length - 1];
      const start = end - 3600;
      const first = times.findIndex((_, i) => buckets[2][i] !== null || buckets[1][i] > 0);
      const gapEnd = first < 0 ? end : times[first] - 30;
      const events = buckets[1].map((v, i) => first < 0 || i < first ? null : v);
      const cores = Number(el.dataset.cores);
      const data = [times, events, buckets[2], times.map(() => cores > 0 ? cores : null)];
      const chart = new uPlot({
        width: Math.max(1, el.clientWidth), height: 180,
        padding: [8, 18, 0, 18], legend: {show: false},
        cursor: {show: false}, select: {show: false},
        scales: {
          x: {time: false, range: () => [start, end]},
          events: {range: (_, min, max) => [0, Math.max(1, max || 0) * 1.15]},
          load: {range: (_, min, max) => [0, Math.max(1, max || 0, cores) * 1.15]}
        },
        axes: [
          {stroke: css("--faint"), grid: {show: false}, ticks: {show: false}, size: 26,
           font: '11px "IBM Plex Mono", monospace', splits: () => [start, start + 900, start + 1800, start + 2700, end],
           values: (_, ticks) => ticks.map(v => v === end ? "now" : "−" + Math.round((end - v) / 60) + "m")},
          {show: false, scale: "events"}, {show: false, scale: "load"}
        ],
        series: [{},
          {label: "Events / min", scale: "events", paths: uPlot.paths.bars({size: [0.65, 12]}), fill: css("--idle"), stroke: css("--idle"), points: {show: false}},
          {label: "Load", scale: "load", stroke: css("--ink"), width: 2, spanGaps: false, points: {show: false}},
          {label: "Cores", scale: "load", stroke: css("--crit"), width: 1, dash: [4, 4], points: {show: false}}
        ],
        hooks: {
          drawClear: [u => {
            if (gapEnd <= start) return;
            const ctx = u.ctx;
            const x = u.valToPos(start, "x", true);
            const right = u.valToPos(gapEnd, "x", true);
            ctx.save(); ctx.fillStyle = css("--sunk");
            ctx.fillRect(x, u.bbox.top, right - x, u.bbox.height);
            ctx.restore();
          }],
          draw: [u => {
            if (gapEnd <= start) return;
            const ctx = u.ctx, ratio = window.devicePixelRatio || 1;
            const available = u.valToPos(gapEnd, "x", true) - u.bbox.left - 16 * ratio;
            ctx.save(); ctx.beginPath(); ctx.rect(u.bbox.left, u.bbox.top, Math.max(0, available), u.bbox.height); ctx.clip();
            ctx.textAlign = "left"; ctx.textBaseline = "middle";
            ctx.fillStyle = css("--faint"); ctx.font = `${11 * ratio}px "IBM Plex Mono", monospace`;
            const text = "no history before collector start";
            if (available >= ctx.measureText(text).width) ctx.fillText(text, u.bbox.left + 8 * ratio, u.bbox.top + u.bbox.height / 2);
            ctx.restore();
          }]
        }
      }, data, el);
      if (first !== 0) {
        const note = document.createElement("p");
        note.className = "chart-note gap-note";
        note.textContent = "Shaded gap: no history before collector start";
        el.after(note);
      }
      const observer = new ResizeObserver(() => {
        if (el.isConnected) chart.setSize({width: Math.max(1, el.clientWidth), height: 180});
      });
      observer.observe(el); plots.set(el, {chart, observer});
    });
  }
  function redraw() {
    document.querySelectorAll(".gap-note").forEach(el => el.remove());
    draw();
  }
  document.addEventListener("DOMContentLoaded", () => {
    applyTheme(); redraw();
    document.getElementById("theme").addEventListener("change", event => {
      preference = event.target.value;
      try { localStorage.setItem("driga-theme", preference); } catch (_) { /* Keep the current choice for this visit. */ }
      applyTheme(); redraw();
    });
    // Repaint once Plex is ready so canvas labels match the rest of the dashboard.
    if (document.fonts) document.fonts.ready.then(redraw);
  });
  document.addEventListener("htmx:afterSwap", redraw);
  system.addEventListener("change", () => { if (preference === "system") redraw(); });
  function connection(connected) {
    const label = document.querySelector(".connection");
    if (label) {
      label.textContent = connected ? "Live · connected" : "Reconnecting…";
      label.classList.toggle("disconnected", !connected);
    }
  }
  document.addEventListener("htmx:sseOpen", () => connection(true));
  document.addEventListener("htmx:sseError", () => connection(false));
})();
