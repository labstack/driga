# Flow map — design

Date: 2026-10-20. Status: look approved by the maintainer (prototype https://claude.ai/artifact/4SViz8ZZucNH8SgJF8tYK6). Changes from the prototype come from reviews by Fable and Codex and are marked as the author's calls.

## Intent

Replace today's Flow view (web and TUI tab 7) with a session map that answers two questions in order: what needs me, and what has this family been doing. It must look professional, be easy to follow, and feel alive, while drawing only what driga actually stores.

## Layout (web and TUI, same meaning)

1. Needs-you strip, machine-wide (not only the selected family): one card per open inbox item with wait age, reason (bounded by capture policy), Jump and Copy resume. When empty, one line: "Nothing needs you · N working".
2. Session map for the selected family: one row per session, lead first, then children by start time. Order never changes while you watch. Time runs left to right and ends at "now".
3. Detail panel for the selected row: state and why (that session's own mixer, not always the lead's), running time, tool calls, tokens and cost, recent events.
4. Terminal: the same three parts with box-drawing characters; `j`/`k` rows, `enter` jump, `[`/`]` family, `-`/`+` zoom.

## What each row draws, and its source

| Mark | Meaning | Source |
|---|---|---|
| Activity band | transcript growth per minute (height); CPU per minute as a thin second track | `observation_minutes` log bytes and CPU, drawn separately, never blended |
| Yellow hatched span with ◆ at its start | waiting on you, from start to answer | inbox `waiting` items: start = `waiting_since_at`, else `occurred_at`; end = min(`ended_at`, `acked_at`), else open |
| Tool ticks, binned per column | tool calls; a bin shows its count on hover | Claude `otlp:tool_result`, Copilot `postToolUse`; none for Codex and Gemini |
| ○ | prompt | Claude `otlp:user_prompt`, Copilot `userPromptSubmitted`; none for Codex and Gemini |
| ● end cap | turn finished | Claude `Stop`, Copilot `agentStop`, Codex and Gemini `handoffs` rows for that session |
| Head at the right edge | last verdict: ▶ working, ◆ needs you, ‖ stalled?, ? unknown, ○ idle, labelled "as of <age>" | stored mixer, as of `mixer.observed_at` |
| Hatched grey | no data: before `retained_from_at`, or inside a collector gap | retention bounds and the `gaps` table |

Where an adapter has no source for a mark, nothing is drawn and the detail panel says so (for example "no tool events for Codex").

Author's calls, from the reviews:
- No reconstructed state history. The prototype guessed idle after 8 minutes without events; that threshold is dropped. Only waits are drawn as spans because only waits are stored as spans. Everything else is measured activity.
- No line back to the lead when a worker finishes. driga cannot prove a result was delivered; the row just ends with ●.
- Every state has a glyph and a pattern as well as a colour, so the view works for colour-blind users and with `NO_COLOR`.
- Stalled and unknown are drawn (head glyph and label) and never shown as "needs you". Codex waits come only from its PermissionRequest hook; Gemini has no wait source, so its silence is drawn from the mixer verdict only.
- A child's row starts at its own `started_at`; the branch only shows `parent_id`, not that the lead started it at that moment.

## Time window

- Now-anchored. Default 60 minutes. Zoom: 15, 60, 240 minutes.
- Retention differs by mode: without the service (snapshot mode) everything older than 59 minutes is pruned; with the service, events keep the newest 200 per session and per-minute rows keep 30 days. Columns before `retained_from_at` hatch, so the 240 zoom in snapshot mode is hatched beyond 60.
- Resolution rule decides the window, not the reverse: at least 4 px per minute on the web and 1 cell per minute in the terminal at the default zoom; if the width cannot fit that, the window shrinks and a left stub says "3h earlier · 2 asks".
- Waits shorter than one column still render as a ◆ mark.

## Scale

- Rows: lead, then children by `started_at` ascending, id as tiebreak. A child is quiet when it has no open wait and no retained event or observation in the window; quiet rows fold into one "+N quiet" row, and expanding lists them.
- More than 8 rows scroll inside the map. No pinning; Jump in the needs-you strip scrolls the map to that row.
- Narrow terminal: below 100 columns the state column drops; below 60 columns the TUI shows the needs-you strip and a list instead of the map.

## Motion

Event-bound only: a short ripple on each new event, a spark along the branch when a worker starts, a glow on the head, a slow pulse on open waits. No perpetual animation while nothing happens. `prefers-reduced-motion` on the web, and a new `motion = off` config key or `NO_COLOR` in the TUI, turn all of it off. The TUI has no clipboard: "Copy resume" shows the command in the detail panel. Redraw on data change and once a second for the clock, not every frame.

## Data contract changes

Per node: `started_at`, `ended_at`, `parent_id`, `mixer` (`derive.SessionMixer`, stored since migration 0009), `events` (retained events in the window, newest 100, from the per-node events `Flow()` already loads, not the 200-event family tail), `waits` (shaped as in the marks table; read with acked items included), `observed` `{from_at, log_bytes_per_min, cpu_ms_per_min}` sized to the window, and `retained_from_at` = the later of the oldest retained event and the oldest observation minute.

Per family: `gaps`, and `attention` `{waits: open waiting items machine-wide with the capture policy applied, working: N}`. Only `waiting` items feed the strip; `finished`, `worker_finished` and `low_disk` do not.

Observations are local only (the fleet feed has no observation kind), so remote nodes have `observed: null` and draw no activity band.

Schema 2 naming (`*_at`, `*_ms`). The web page gets the same FlowView from a new `GET /flow.json` and refetches on the SSE `refresh` event; the TUI and MCP `get_flow` use the same struct.

## Testing

- Golden TUI frames at 80, 100 and 140 columns, with and without colour.
- Web: JSON-to-geometry unit tests (row order stable across updates, waits drawn from inbox spans, no span without a stored source, binning).
- A fixture family spanning 8 hours with dense tool calls and 12 quiet threads; in snapshot mode most of it must render hatched, and the test asserts that.

## Left to the builder

Canvas versus SVG on the web, exact colours within the existing theme tokens, the hover card layout.
