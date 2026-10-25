"use strict";
(() => {
  async function answerPrompt(button) {
    const row=button.closest("[data-prompt]"), status=row.querySelector('[role="status"]');
    row.querySelectorAll("button").forEach(el=>el.disabled=true);
    try {
      const response=await fetch("/chat/api/prompt-answer",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({id:button.dataset.promptId,allow:button.dataset.promptAnswer==="allow"})});
      if(!response.ok)throw new Error(await response.text());
      row.remove();
    } catch(error) {status.textContent=error.message;row.querySelectorAll("button").forEach(el=>el.disabled=false);}
  }
  const readOnly = "read-only in browser · use the CLI";
  const states = ["working", "waiting", "stalled", "idle", "finished", "ended", "dead", "unknown"];
  function parseCommand(input, scope = "all") {
    const error = message => ({error: message});
    if (input.length > 500) return error("Command is too long");
    // Tokenization supports quoted project names; nothing here is executable code.
    const tokens = []; let token = "", quote = "", started = false;
    for (const c of input.trim()) {
      if (quote) { if (c === quote) quote = ""; else token += c; started = true; }
      else if (c === '"' || c === "'") { quote = c; started = true; }
      else if (/\s/.test(c)) { if (started) tokens.push(token); token = ""; started = false; }
      else { token += c; started = true; }
    }
    if (quote) return error("Close the quoted project name");
    if (started) tokens.push(token);
    const verb = tokens.shift();
    if (["ack", "kill"].includes(verb) || verb === "hooks" ||
        ["agents", "projects", "inbox", "orphans", "cleanup"].includes(verb) && tokens.some(t => ["ack", "kill", "jump", "remove", "summarize"].includes(t))) return error(readOnly);
    const query = {machine: scope};
    function url(path) {
      return {url: path + "?" + Object.keys(query).sort().map(k => encodeURIComponent(k) + "=" + encodeURIComponent(query[k]).replace(/%20/g, "+")).join("&")};
    }
    if (verb === "flow") {
      if (tokens.length > 1 || tokens.length && !/^[1-9][0-9]*$/.test(tokens[0])) return error("Use flow [<id>]");
      if (tokens.length) query.id = tokens[0]; return url("/flow");
    }
    if (["agents", "projects", "inbox", "orphans", "machines", "cleanup"].includes(verb) && tokens[0] === "list") tokens.shift();
    if (verb === "agents" && tokens[0] === "show") {
      if (tokens.length !== 2 || !/^[1-9][0-9]*$/.test(tokens[1])) return error("Use agents show <session-id>");
      return url("/agents/" + tokens[1]);
    }
    if (verb === "agents") {
      while (tokens.length) {
        const flag = tokens.shift();
        if (flag === "--all" && !query.all) query.all = "1";
        else if (flag === "--state" && !query.state && states.includes(tokens[0])) query.state = tokens.shift();
        else if (flag === "--project" && !query.project && tokens[0] && !tokens[0].startsWith("--")) query.project = tokens.shift();
        else return error("Use agents [--state <state>] [--all] [--project <name>]");
      }
      return url("/agents");
    }
    if (verb === "projects" && tokens[0] === "show" && tokens.length > 1) { query.project = tokens.slice(1).join(" "); return url("/projects"); }
    if (verb === "cleanup" && tokens.length === 1 && tokens[0] === "--why") { query.why = "1"; return url("/cleanup"); }
    if (!tokens.length && ["home", "projects", "inbox", "orphans", "machines", "cleanup", "activity"].includes(verb)) return url(verb === "home" ? "/" : "/" + verb);
    return error("Unknown command · ? for help");
  }
  function createMachineController(doc) {
    let expanded = new Set();
    const rows = () => Array.from(doc.querySelectorAll("[data-machine-app]"));
    const key = row => {
      const rank = row.closest("[data-machine-rank]").dataset;
      return JSON.stringify([rank.machineId, rank.machineRank, row.dataset.machineApp]);
    };
    return {
      capture() { expanded = new Set(rows().filter(row => row.open).map(key)); },
      restore() { for (const row of rows()) row.open = expanded.has(key(row)); }
    };
  }
  function createInboxController(doc, viewport) {
    let expanded = new Set(), limit = 50, position = null;
    const allRows = () => Array.from(doc.querySelectorAll("[data-inbox-key]"));
    function render() {
      let recent = 0;
      for (const row of allRows()) {
        const capped = row.hasAttribute("data-inbox-recent");
        const pane = row.closest("[data-pane]");
        const field = pane && pane.querySelector(".filter input");
        const matches = !field || row.textContent.toLowerCase().includes(field.value.toLowerCase());
        // Keep an expanded row visible if fresh completions push it past the cap.
        row.hidden = !matches || (capped && recent++ >= limit && !row.open);
      }
      const older = doc.querySelector("[data-inbox-older]");
      if (older) {
        const remaining = Math.max(0, doc.querySelectorAll("[data-inbox-recent]").length - limit);
        older.hidden = remaining === 0;
        older.textContent = "show older · next " + Math.min(50, remaining) + " (" + remaining + " remaining)";
      }
    }
    return {
      render,
      capture() {
        if (!allRows().length) return;
        expanded = new Set(allRows().filter(row => row.open).map(row => row.dataset.inboxKey));
        position = {x:viewport.scrollX, y:viewport.scrollY, panes:Array.from(doc.querySelectorAll("[data-pane]")).map(p => [p.scrollLeft, p.scrollTop])};
      },
      restore() {
        for (const row of allRows()) row.open = expanded.has(row.dataset.inboxKey);
        render();
        if (position) {
          doc.querySelectorAll("[data-pane]").forEach((p,i) => { if (position.panes[i]) [p.scrollLeft,p.scrollTop] = position.panes[i]; });
          viewport.scrollTo(position.x, position.y);
        }
      },
      showOlder() { limit += 50; render(); }
    };
  }
  function connectionText(connected) { return connected?"● live":"○ reconnecting…"; }
  globalThis.DrigaConsole = Object.freeze({answerPrompt,connectionText,parseCommand, createInboxController, createMachineController});
  if (typeof document === "undefined") return;

  const root = document.documentElement;
  const inboxUI = createInboxController(document, window);
  const machineUI = createMachineController(document);
  const system = matchMedia("(prefers-color-scheme: light)");
  const phone = matchMedia("(max-width: 900px)");
  const css = name => getComputedStyle(root).getPropertyValue("--" + name).trim();
  let history = [], historyIndex = 0, draft = "";
  try {
    const stored = JSON.parse(localStorage.getItem("driga-command-history") || "[]");
    if (Array.isArray(stored)) history = stored.filter(s => typeof s === "string" && s.length <= 500).slice(-100);
  } catch (_) { /* Storage is optional. */ }

  // The only ECharts owner: canvas + richText tooltip keeps CSP free of injected HTML/styles.
  const charts = {
    plots: new Map(),
    dispose() { for (const {chart, observer} of this.plots.values()) { observer.disconnect(); chart.dispose(); } this.plots.clear(); },
    options(el) {
      const [times, counts, loads] = JSON.parse(el.dataset.buckets);
      const cores = Number(el.dataset.cores), end = times[times.length - 1];
      const first = times.findIndex((_, i) => loads[i] !== null || counts[i] > 0);
      const points = values => times.map((t, i) => [t * 1000, values[i]]);
      const events = counts.map((n, i) => first < 0 || i < first ? null : n);
      const gapEnd = first < 0 ? end : times[first];
      return {
        animation: false, textStyle: {fontFamily: '"IBM Plex Mono", monospace', color: css("fg")},
        aria: {enabled: true, description: "Events per minute and load over sixty minutes. Missing samples remain gaps; dashed line shows cores."},
        grid: {left: 2, right: 8, top: 8, bottom: phone.matches ? 26 : 42},
        tooltip: {trigger: "axis", renderMode: "richText", confine: true, backgroundColor: css("bg"), borderColor: css("accent"), borderWidth: 1,
          textStyle: {fontFamily: '"IBM Plex Mono", monospace', color: css("fg"), fontSize: 12},
          formatter: params => {
            if (!params.length) return "";
            const t = Number(params[0].value[0]);
            const lines = ["−" + Math.round((end * 1000 - t) / 60000) + "m · " + new Date(t).toLocaleTimeString([], {hour: "2-digit", minute: "2-digit"})];
            for (const p of params) lines.push(p.seriesName + " " + (p.value[1] === null ? "unavailable" : p.value[1]));
            return lines.join("\n");
          }},
        xAxis: {type: "value", min: (end - 3600) * 1000, max: end * 1000, interval: 900000, axisLine: {show: false}, axisTick: {show: false}, splitLine: {show: false},
          axisLabel: {color: css("dim"), fontSize: 11, formatter: t => t >= end * 1000 ? "now" : "−" + Math.round((end * 1000 - t) / 60000) + "m"}},
        yAxis: [{type: "value", show: false, min: 0}, {type: "value", min: 0, max: value => Math.max(1, value.max, cores) * 1.15,
          axisLabel: {show: false}, axisLine: {show: false}, axisTick: {show: false}, splitLine: {lineStyle: {color: css("line"), type: "dotted"}}}],
        dataZoom: [{type: "inside", filterMode: "none"}, {type: "slider", show: !phone.matches, height: 12, bottom: 0, borderColor: css("line"), backgroundColor: css("pane"), fillerColor: css("raise"), textStyle: {color: css("dim")}, showDetail: false, handleStyle: {color: css("accent"), borderColor: css("accent")}, moveHandleStyle: {color: css("dim")}, dataBackground: {lineStyle: {color: css("dim")}, areaStyle: {color: css("line")}}, selectedDataBackground: {lineStyle: {color: css("idle")}, areaStyle: {color: css("line")}}}],
        series: [
          {name: "events/min", type: "bar", data: points(events), barMaxWidth: 10, itemStyle: {color: css("idle")},
            markArea: {silent: true, itemStyle: {color: css("raise")}, data: gapEnd > end - 3600 ? [[{xAxis: (end - 3600) * 1000}, {xAxis: gapEnd * 1000}]] : []}},
          {name: "load", type: "line", yAxisIndex: 1, data: points(loads), connectNulls: false, showSymbol: false, lineStyle: {color: css("fg"), width: 1.5}, itemStyle: {color: css("fg")},
            markLine: {silent: true, symbol: "none", label: {show: false}, lineStyle: {color: css("crit"), type: "dashed", width: 1}, data: cores > 0 ? [{yAxis: cores}] : []}}
        ]
      };
    },
    draw() {
      this.dispose();
      if (!window.echarts) return;
      document.querySelectorAll(".chart[data-buckets]").forEach(el => {
        if (!JSON.parse(el.dataset.buckets)[0].length) return;
        const chart = echarts.init(el, null, {renderer: "canvas"}); chart.setOption(this.options(el));
        const observer = new ResizeObserver(() => { if (el.isConnected) chart.resize(); });
        observer.observe(el); this.plots.set(el, {chart, observer});
      });
    }
  };

  let focused = 0, selected = "", paused = false, tailStarted = false, frozenTail = [];
  const tailNodes = new Map(), filters = new Map();
  const panes = () => Array.from(document.querySelectorAll("[data-pane]"));
  const rows = pane => Array.from(pane.querySelectorAll("[data-row]")).filter(el => !el.hidden && !el.parentElement.closest("[data-row]"));
  function focusPane(index, focus = true) {
    const all = panes(); if (!all.length) return;
    focused = (index + all.length) % all.length;
    all.forEach((p, i) => p.classList.toggle("focus", i === focused));
    if (focus) all[focused].focus({preventScroll: true});
  }
  function selectRow(pane, index, scroll = true) {
    const all = rows(pane); if (!all.length) return;
    const row = all[(index + all.length) % all.length];
    all.forEach(el => el.classList.toggle("sel", el === row));
    selected = row.getAttribute("href") || row.dataset.session || String(all.indexOf(row));
    if (scroll) row.scrollIntoView({block: "nearest"});
  }
  function filterPane(pane, focus = true) {
    const paneIndex = panes().indexOf(pane);
    let label = pane.querySelector(".filter");
    if (!label) {
      label = document.createElement("label"); label.className = "filter"; label.textContent = "/ ";
      const input = document.createElement("input"); input.type = "search"; input.setAttribute("aria-label", "Filter focused pane");
      input.addEventListener("input", () => {
        filters.set(paneIndex, input.value);
        pane.querySelectorAll("[data-row]").forEach(row => { row.hidden = !row.textContent.toLowerCase().includes(input.value.toLowerCase()); });
        inboxUI.render();
      });
      label.append(input); pane.prepend(label);
    }
    if (focus) label.querySelector("input").focus();
  }
  function renderTail() {
    const tail = document.querySelector("[data-tail]");
    const label = document.querySelector("[data-tail-status]");
    if (label) label.textContent = paused ? "paused · p resume" : "live · p pause";
    const button = document.querySelector("[data-pause-tail]"); if (button) button.textContent = paused ? "p resume" : "p pause";
    if (!tail) return;
    const display = paused ? frozenTail.map(el => el.cloneNode(true)) : Array.from(tailNodes.values());
    const query = (filters.get(panes().indexOf(tail.closest("[data-pane]"))) || "").toLowerCase();
    for (const row of display) row.hidden = !row.textContent.toLowerCase().includes(query);
    if (display.length) { tail.replaceChildren(...display); if (!paused) tail.scrollTop = tail.scrollHeight; }
  }
  function ingestTail(events) {
    for (const e of events) {
      if (tailNodes.has(e.key)) continue;
      const row = document.createElement("div"); row.dataset.row = ""; row.dataset.eventKey = e.key;
      row.title = [e.machine, e.adapter, e.kind, e.summary].join(" · ");
      const time = document.createElement("span"); time.className = "d"; time.textContent = new Date(e.occurred_at).toLocaleTimeString([], {hour12: false});
      const agent = document.createElement("span"); agent.className = "m"; agent.textContent = e.adapter;
      const kind = document.createElement("span"); kind.className = ["working", "waiting", "stalled", "idle", "finished", "ended", "dead"].includes(e.state) ? e.state : "finished"; kind.textContent = e.kind;
      row.append(time, " ", agent, " ", kind, " ", document.createTextNode(e.summary)); tailNodes.set(e.key, row);
    }
    while (tailNodes.size > 200) tailNodes.delete(tailNodes.keys().next().value);
    renderTail();
  }
  function pauseTail() {
    if (!paused) { const tail = document.querySelector("[data-tail]"); frozenTail = tail ? Array.from(tail.children).map(el => el.cloneNode(true)) : []; }
    paused = !paused; renderTail();
  }

  const input = document.getElementById("cmdline"), menu = document.getElementById("command-menu"), error = document.getElementById("command-error"), help = document.getElementById("help");
  let suggestions = [], choice = -1;
  const commands = ["flow", "flow <id>", "agents", ...states.map(s => "agents --state " + s), "agents --all", "agents --project <name>", "projects show <project>", "projects", "inbox", "orphans", "machines", "cleanup", "cleanup --why", "activity", "agents show <session-id>", "agents jump <session-id>", "agents kill <session-id>", "inbox ack <item-id>", "inbox jump <item-id>", "orphans kill <orphan-id>", "cleanup remove <candidate-id>", "hooks install"];
  const mutating = command => parseCommand(command).error === readOnly;
  function closeMenu() { menu.hidden = true; input.setAttribute("aria-expanded", "false"); input.removeAttribute("aria-activedescendant"); choice = -1; }
  function complete() {
    const q = input.value.trim(); suggestions = commands.filter(c => !q || c.includes(q)).slice(0, 8); choice = -1;
    menu.replaceChildren();
    suggestions.forEach((command, i) => {
      const option = document.createElement("button"); option.type = "button"; option.className = "opt" + (mutating(command) ? " ro" : ""); option.id = "command-option-" + i; option.setAttribute("role", "option"); option.setAttribute("aria-selected", "false"); if (mutating(command)) option.setAttribute("aria-disabled", "true");
      option.textContent = command; const hint = document.createElement("span"); hint.textContent = mutating(command) ? readOnly : "view"; option.append(hint);
      option.addEventListener("click", () => { if (mutating(command)) return; input.value = command; input.focus(); closeMenu(); }); menu.append(option);
    });
    menu.hidden = !suggestions.length; input.setAttribute("aria-expanded", String(!menu.hidden));
  }
  function menuChoice(direction) {
    if (!suggestions.length) return;
    choice = (choice + direction + suggestions.length) % suggestions.length;
    Array.from(menu.children).forEach((el, i) => { el.classList.toggle("hi", i === choice); el.setAttribute("aria-selected", String(i === choice)); });
    input.setAttribute("aria-activedescendant", "command-option-" + choice);
  }
  function showError(message) { error.textContent = message; error.hidden = false; input.setAttribute("aria-invalid", "true"); }
  function selectedFlowURL(target) {
    const url = new URL(target, location.href);
    if (url.pathname !== "/flow" || url.searchParams.has("id")) return target;
    const row = document.querySelector("[data-row].sel[href*='/agents/']");
    const path = row ? new URL(row.href).pathname : location.pathname;
    if (/^\/agents\/[1-9][0-9]*$/.test(path)) url.searchParams.set("id", path.split("/").pop());
    return url.toString();
  }
  function runCommand() {
    const text=input.value.trim(), verb=text.split(/\s+/)[0];
    if (text && !["home","flow","agents","projects","inbox","orphans","machines","cleanup","activity","ack","kill","hooks"].includes(verb) && window.drigaChat) {window.drigaChat.open(text);input.value="";closeMenu();return;}
    const scope = document.querySelector(".status").dataset.scope || "all";
    const result = parseCommand(input.value, scope);
    if (result.error) {
 if (result.error === "Unknown command · ? for help" && window.drigaChat) { window.drigaChat.open(input.value); input.value=""; closeMenu(); return; }
 showError(result.error); return; }
    history.push(input.value); history = history.slice(-100); historyIndex = history.length;
    try { localStorage.setItem("driga-command-history", JSON.stringify(history)); } catch (_) { /* History is optional. */ }
    error.hidden = true; input.removeAttribute("aria-invalid"); closeMenu();
    window.location.assign(selectedFlowURL(result.url));
  }
  document.getElementById("command-form").addEventListener("submit", event => { event.preventDefault(); runCommand(); });
  input.addEventListener("input", () => { complete(); error.hidden = true; input.removeAttribute("aria-invalid"); });
  input.addEventListener("focus", () => { historyIndex = history.length; draft = input.value; complete(); });
  input.addEventListener("keydown", event => {
    if (event.key === "Escape") { closeMenu(); input.blur(); focusPane(focused); event.preventDefault(); }
    if (event.key === "Tab" && !menu.hidden) { event.preventDefault(); menuChoice(event.shiftKey ? -1 : 1); }
    if (event.key === "Enter" && choice >= 0 && !menu.hidden) {
      event.preventDefault(); const command = suggestions[choice];
      if (mutating(command)) { showError(readOnly); return; }
      input.value = command; closeMenu(); return;
    }
    if (event.key === "ArrowUp" || event.key === "ArrowDown") {
      event.preventDefault(); if (historyIndex === history.length) draft = input.value;
      historyIndex = Math.max(0, Math.min(history.length, historyIndex + (event.key === "ArrowUp" ? -1 : 1)));
      input.value = historyIndex === history.length ? draft : history[historyIndex]; closeMenu();
    }
  });
  function closeHelp() { help.close(); focusPane(focused); }
  // The feedback notice sits outside #main, so refreshes keep it.
  let noticeTimer = 0;
  function notice(text) {
    const el = document.getElementById("app-notice"); if (!el) return;
    el.textContent = text; el.classList.add("show");
    clearTimeout(noticeTimer); noticeTimer = setTimeout(() => el.classList.remove("show"), 4000);
  }
  async function jumpTo(button) {
    button.disabled = true;
    try {
      const response = await fetch("/chat/api/flow-action", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({action: "jump", session_id: Number(button.dataset.session), machine: button.dataset.machine})});
      if (!response.ok) throw new Error(await response.text());
      notice("Opened terminal");
    } catch (e) { notice(e.message); } finally { button.disabled = false; }
  }
  // The app's menus and notifications drive the page through the same
  // functions its keys call.
  function openView(name) {
    if (name === "chat") { if (window.drigaChat) window.drigaChat.toggle(); return; }
    const link = document.querySelector('[data-view="' + name + '"]');
    if (link) window.location.assign(name === "flow" ? selectedFlowURL(link.href) : link.href);
  }
  function focusFind() {
    const board = document.querySelector("[data-board-search]");
    if (board) { board.focus(); return; }
    const pane = panes()[focused]; if (pane) filterPane(pane);
  }
  function showSession(id) {
    id = String(id); if (!/^[1-9][0-9]*$/.test(id)) return;
    if (document.querySelector('[data-flow-board] [data-session="' + id + '"]')) { document.dispatchEvent(new CustomEvent("driga:select-session", {detail: {id: Number(id)}})); return; }
    window.location.assign("/flow?" + new URLSearchParams({id, machine: document.querySelector(".status").dataset.scope || "all"}));
  }
  const commandsByName = {view: openView, find: focusFind, reload: () => window.location.reload(), openSession: showSession};
  window.drigaCommand = (name, arg) => { if (Object.hasOwn(commandsByName, name)) commandsByName[name](arg); };
  document.querySelector("[data-close-help]").addEventListener("click", closeHelp);
  document.addEventListener("click", event => {
    const promptButton=event.target.closest("[data-prompt-answer]");
    if(promptButton){event.preventDefault();answerPrompt(promptButton);return;}
    const jump = event.target.closest("[data-inbox-jump]");
    if (jump) { event.preventDefault(); jumpTo(jump); return; }
    if (event.target.closest("[data-inbox-older]")) { inboxUI.showOlder(); return; }
    if (!event.target.closest(".cmd")) closeMenu();
    if (event.target.closest("[data-pause-tail]")) pauseTail();
    const flowNav = event.target.closest('[data-view="flow"]');
    if (flowNav) flowNav.href = selectedFlowURL(flowNav.href);
    const pane = event.target.closest("[data-pane]");
    if (pane) { focusPane(panes().indexOf(pane), false); const row = event.target.closest("[data-row]"); if (row) selectRow(pane, rows(pane).indexOf(row), !row.matches("[data-inbox-key]")); }
  });
  document.addEventListener("focusin", event => {
    const pane = event.target.closest("[data-pane]"); if (pane) focusPane(panes().indexOf(pane), false);
  });
  document.addEventListener("driga:select-session", event => {
    const id=String(event.detail.id);
    const row=Array.from(document.querySelectorAll("[data-row]")).find(n=>(n.getAttribute("href")||"").split("?")[0]==="/agents/"+id);
    if(row){const pane=row.closest("[data-pane]");if(pane){focusPane(panes().indexOf(pane),false);selectRow(pane,rows(pane).indexOf(row),false);}row.scrollIntoView({block:"nearest"});}
  });
  document.addEventListener("keydown", event => {
    if (event.defaultPrevented || event.ctrlKey || event.altKey || event.metaKey || event.isComposing) return;
    if (help.open) { if (event.key === "?" || event.key === "Escape") { event.preventDefault(); closeHelp(); } return; }
    if (event.target.closest("input,textarea,select,[contenteditable='true']")) {
      if (event.key === "Escape" && event.target.closest(".filter")) { event.preventDefault(); filters.delete(focused); const pane = panes()[focused]; pane.querySelectorAll("[data-row]").forEach(el => { el.hidden = false; }); event.target.closest(".filter").remove(); inboxUI.render(); focusPane(focused); }
      return;
    }
    if(document.querySelector("[data-flow-board]")&&!event.target.closest(".fb-activity")&&["j","k","ArrowDown","ArrowUp","ArrowLeft","ArrowRight","Home","End","Enter"," ","/"].includes(event.key))return;
    const pane = panes()[focused];
    if (/^[0-2]$/.test(event.key)) { event.preventDefault(); openView(["flow", "projects", "machines"][Number(event.key)]); }
    else if (event.key === ":") { event.preventDefault(); input.focus(); }
    else if (event.key === "?") { event.preventDefault(); help.showModal(); }
    else if (event.key === "p") { event.preventDefault(); pauseTail(); }
    else if (pane && ["j", "k", "ArrowDown", "ArrowUp"].includes(event.key)) {
      event.preventDefault(); const all = rows(pane), current = all.findIndex(r => r.classList.contains("sel"));
      selectRow(pane, current < 0 ? 0 : current + (["j", "ArrowDown"].includes(event.key) ? 1 : -1));
    } else if (pane && event.key === "Enter" && event.target === pane) {
      event.preventDefault(); const row = pane.querySelector("[data-row].sel") || rows(pane)[0]; const link = row && (row.matches("a") ? row : row.querySelector(row.matches("[data-inbox-key]") ? "summary" : "a")); if (link) link.click();
    } else if (pane && event.key === "/") { event.preventDefault(); filterPane(pane); }
    else if (event.key === "Tab" && panes().length > 1 && pane && !pane.matches(".inbox-pane") && (event.target.matches("[data-pane]") || event.target.closest("[data-row]"))) { event.preventDefault(); focusPane(focused + (event.shiftKey ? -1 : 1)); }
    else if (event.key === "Escape") { closeMenu(); if (pane) { pane.querySelectorAll(".sel").forEach(el => el.classList.remove("sel")); focusPane(focused); } }
  });
  function refreshUI() {
    focusPane(focused, false);
    panes().forEach((pane, i) => {
      for (const row of pane.querySelectorAll("[data-row]")) {
        if (!row.title && !row.matches("[data-inbox-key]")) row.title = row.textContent.trim();
        row.querySelectorAll(".inbox-columns [role='cell']").forEach(el => { el.title = el.textContent; });
        row.querySelectorAll(".t,.l,.branch,.item p,dd").forEach(el => { if (!el.title) el.title = el.textContent; });
        if (row.getAttribute("href") === selected) row.classList.add("sel");
      }
      if (filters.has(i)) { const value = filters.get(i); filterPane(pane, false); const field = pane.querySelector(".filter input"); field.value = value; pane.querySelectorAll("[data-row]").forEach(row => { row.hidden = !row.textContent.toLowerCase().includes(value.toLowerCase()); }); }
    });
    const tail = document.querySelector("[data-tail]");
    if (tail && !tailStarted) { tail.querySelectorAll("[data-event-key]").forEach(el => tailNodes.set(el.dataset.eventKey, el)); tailStarted = true; }
    renderTail(); charts.draw();
  }
  let restorePane = false;
  document.addEventListener("htmx:beforeSwap", () => { restorePane = !document.activeElement.closest("[data-flow-board]") && !!document.activeElement.closest("[data-pane]"); inboxUI.capture(); machineUI.capture(); charts.dispose(); });
  document.addEventListener("htmx:afterSwap", () => { refreshUI(); inboxUI.restore(); machineUI.restore(); if (restorePane) focusPane(focused); });
  refreshUI();
  inboxUI.restore();
  if (document.fonts) document.fonts.ready.then(() => charts.draw());
  system.addEventListener("change", () => { if (root.dataset.theme === "system") charts.draw(); });
  phone.addEventListener("change", () => charts.draw());
  function connection(connected) { document.querySelectorAll(".connection").forEach(el => { el.textContent = connectionText(connected); el.classList.toggle("working", connected); el.classList.toggle("waiting", !connected); }); }
  const refresh = () => { const main = document.getElementById("main"); if (window.htmx) htmx.trigger(main, "console:refresh"); };
  // One long poll at a time: the first answer refreshes and fills the tail,
  // later answers carry data changes and new tail rows.
  const updates = new AbortController();
  (async () => {
    let cursor = "";
    while (!updates.signal.aborted) {
      try {
        const query = new URLSearchParams(location.search); if (cursor) query.set("after", cursor);
        const response = await fetch("/updates?" + query, {signal: updates.signal});
        // A refused cursor (a restarted server) starts over with a fresh one.
        if (!response.ok) { if (response.status >= 400 && response.status < 500) cursor = ""; throw new Error(response.statusText); }
        const update = await response.json();
        connection(true);
        if (!cursor || update.refresh) refresh();
        cursor = update.cursor; if (update.tail.length) ingestTail(update.tail);
      } catch (_) {
        if (updates.signal.aborted) return;
        connection(false); await new Promise(resolve => setTimeout(resolve, 1000));
      }
    }
  })();
  let promptPolling=false;
  setInterval(async()=>{
    const container=document.querySelector("[data-prompts]");
    if(!container||promptPolling)return;
    promptPolling=true;
    try {
      const response=await fetch("/chat/api/prompts");
      if(!response.ok)return;
      const prompts=await response.json();
      const shown=Array.from(container.querySelectorAll("[data-prompt]"),row=>row.dataset.prompt).sort();
      if(JSON.stringify(shown)!==JSON.stringify(prompts.map(prompt=>prompt.id).sort()))refresh();
    } catch(_) { /* The next poll retries. */ } finally {promptPolling=false;}
  },1000);
  const clock = setInterval(() => { document.querySelectorAll("[data-clock]").forEach(el => { el.textContent = new Date().toLocaleTimeString([], {hour12: false}); }); }, 1000);
  window.addEventListener("pagehide", () => { updates.abort(); charts.dispose(); clearInterval(clock); });
})();
