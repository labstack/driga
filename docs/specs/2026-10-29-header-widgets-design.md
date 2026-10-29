# Header widgets: add and remove, by hand or through Chat

Date: 2026-10-29. Status: draft for review.

## Intent

The owner wants the desktop app's header to be dynamic. Anyone using the
app should be able to put any widget that fits in the header there, and
take any of them out again, either by hand or by asking Chat. The trigger
was CPU and disk: the owner expected to see them in the header and could
not add them.

"Dynamic" here means the header is whatever the stored header spec says,
not a fixed set of tiles with switches. Every value Driga can reduce to one
number is a candidate, not only a curated list.

## What exists today

- The header is already a stored dashboard: id `header`, shipped by
  `panel.Header()` in `internal/panel/home.go`. It is versioned in the
  dashboard store like Home, Chat and the MCP panel tools can edit it, and
  deleting it restores the shipped spec. `panel.ValidateHeader` allows at
  most 8 panels, all `stat`.
- The app fetches that spec, but `ui/src/host/ClustersHeader.tsx` ignores
  its panel list. It draws six hard-coded tiles and reads their values by
  fixed id (`waiting`, `finished`, `stuck`, `leftover`, `reclaim`,
  `memory`) through `stat()` in `ui/src/host/Header.ts`. Adding a panel
  through Chat shows nothing, and removing one shows a zero.
- The machine dots and their count come from the fleet data, not from the
  spec.
- The data is already there. `summary`, `local_activity` and `machines`
  carry CPU %, disk free and total, load, swap, memory, agent counts by
  state, inbox, orphans, leftover and reclaimable bytes.

## Goals

1. The header renders the stored header spec: its panels, in its order,
   with its titles. A change saved by any author (the person, Chat, an MCP
   client) shows up on the next refresh without reloading the app.
2. Any `stat` or `gauge` panel over any source and numeric measure can be
   a header widget. A `stat` with a `total` draws a value-of-total bar, as
   Memory does today. A `gauge` draws a ring (`RingGauge`). A stat with
   history draws the existing trend line and 1h delta.
3. A catalog of ready-made widgets covers every useful single number, so
   most people never write a spec. See "Widget catalog".
4. Manual editing in the app:
   - each tile's menu gets "Remove from header", next to "Show on Home"
     and "Ask Chat about it";
   - an "Add widget" control at the end of the tile row opens a searchable
     picker of catalog widgets not already shown, plus "Ask Chat for
     another…", which opens Chat with a starter prompt;
   - tiles reorder by dragging;
   - each change saves as one dashboard version, and a toast offers Undo,
     which restores the previous version;
   - "Reset header" in the add control's menu saves the shipped header as a new
     version with message "Reset to default", preserving history and Undo.
5. Chat editing: "add CPU to the header", "drop Reclaimable", "put Disk
   before Memory" work through the existing dashboard edit operations on
   id `header`. Chat gets the catalog in its tool description, so it uses
   the same ids and specs as the picker.
6. Behaviors tied to particular tiles keep working wherever those tiles
   sit:
   - Need you, To review and Stuck stay attention tiles. Their tone, pulse,
     click-to-filter-the-inbox and keyboard shortcuts depend on the
     panel's measure (`summary` `need_you`, `to_review`,
     `attention_stuck`), not on its position or id.
   - "Show on Home" opens the matching Home panel when there is one, and
     otherwise adds this widget's panel to Home.

## Non-goals

- Charts, tables, lists, the inbox, cards, timelines, bar charts, the
  heatmap, swimlanes and the sankey stay off the header. They need more
  room than one 56 px row has.
- Per-machine or per-user headers. There is one header spec per Driga data
  directory, as today.
- New data sources or new measurements. Everything in the catalog already
  exists in a source.
- Changing what Home shows by default.

## Anti-goals (a failure even if it technically works)

- The header grows taller than today's 56 px or wraps onto a second line.
- A widget shows `0` when its data is missing. Missing or unknown values
  show as unknown (`—`), the way CPU's first poll does.
- Removing the attention tiles silently loses the inbox filter. When none
  of the three are in the header, the inbox still has its own filter
  controls.
- A Chat edit changes the header without the person seeing it happen.
  Agent-authored header edits follow Chat's current mode, like other Chat
  actions.

## Widget catalog

Every entry is an ordinary header panel spec. The ids are stable, so the
picker can tell what is already shown and Chat can name them. The first
six entries are the shipped default, in order: today's six tiles, with
Leftover (count and bytes) as one widget.

| id | Title | Viz | Source | Measure / total |
|---|---|---|---|---|
| `waiting` | Need you | stat | summary | `sum:need_you` |
| `finished` | To review | stat | summary | `sum:to_review` |
| `stuck` | Stuck | stat | summary | `sum:attention_stuck` |
| `leftover` | Leftover | stat | summary | `sum:leftover_bytes`, caption `sum:leftover` |
| `reclaim` | Reclaimable | stat | summary | `sum:reclaim_bytes` |
| `memory` | Memory | stat | local_activity | `max:mem_used_bytes` / `max:mem_total_bytes` |
| `cpu` | CPU | gauge | local_activity | `max:cpu_percent`, max 100 |
| `disk` | Disk used | stat | local_activity | `max:disk_used_bytes` / `max:disk_total_bytes` |
| `load` | Load | stat | local_activity | `max:load1` |
| `swap` | Swap | stat | local_activity | `max:swap_used_bytes` |
| `events` | Events/min | stat | local_activity | `max:events` |
| `agents` | Agents | stat | summary | `sum:agents` |
| `working` | Working | stat | summary | `sum:working` |
| `waiting_now` | Waiting | stat | summary | `sum:waiting` |
| `inbox` | Inbox | stat | summary | `sum:inbox` |
| `orphans` | Leftover trees | stat | summary | `sum:orphans` |
| `machines_up` | Machines up | stat | machines | `count` where state is reachable, total `count` |
| `agent_memory` | Agent memory | stat | costs | `sum:mem_bytes` |

The builder adds any further single-number measure that the sources
already expose and this table missed.

Settled details for the catalog:

- **Current value.** Over a source with a `ts` column (`local_activity`),
  `internal/panel/run.go` already makes a stat or gauge the latest reading,
  with the trend leading to it. CPU, Memory, Load, Swap, Events and Disk
  therefore show the current value and get a trend line without new code.
  Widgets over `summary` show the current total. They get a trend wherever
  the existing panel history covers them, and none otherwise. A missing
  trend is fine, but an invented one is not.
- **Machine scope.** This stays as today. Count widgets (`summary`) follow
  the app's machine selection. Machine-health widgets (`local_activity`)
  describe the machine the app runs on, and their tile shows that
  machine's name as the sublabel, as Memory does now. (My call.)
- **Disk.** The `activity`, `local_activity`, `summary` and `machines`
  sources gain a derived `disk_used_bytes` column (total − free, unknown
  when either is unknown). `disk` is then
  `max:disk_used_bytes` / `max:disk_total_bytes` over `local_activity`,
  with a value-of-total bar. Chat and Home can use the column too. (My
  call: one column is simpler for Chat to express than a new spec field.)
- **Leftover.** Stat panels gain an optional `caption` measure, shown
  after the value: `leftover` is `sum:leftover_bytes` with caption
  `sum:leftover`, the tree count, which keeps today's "2 · 1.8 GB" look.
  The title becomes "Leftover". (My call: a general field, not a UI
  special case.)
- **Machines up.** `machines_up` counts rows where `state` is `local` or
  `live` (see `view.MachineState`), with total `count`, drawn as a
  value-of-total bar.

The machine dots and count are not a widget. They stay fixed beside
search, with the brand mark and the Chat toggle. (My call: the dots are a
fleet summary, not one number, and the Fleet page is one click away.)

## Limits and overflow

- The stored limit rises from 8 to 16 panels (my call), and `gauge`
  becomes valid in `ValidateHeader`.
- The row never wraps. When the tiles do not fit, the trailing tiles move
  into a "+N" menu at the end of the row, which lists them with their
  current values. The person controls what falls off first by reordering.
  Today's `visibleFrom` hiding by breakpoint is replaced by this.

## Where things live

- **The catalog is in Go.** It sits next to `panel.Header()` and is the
  one source of the specs. The app's picker reads it through a dashboard
  API route. The MCP `list` tool description, which already tells agents
  about the `header` dashboard, is generated from it and corrected (it
  says "only stat panels, at most 8" today). The panel spec's `source`
  doc comment also gains the missing `local_activity`.
- **Refresh.** The app already re-opens the header spec every 5 seconds
  while visible (`useDashboardRefresh`, `refreshMs`). An edit from Chat
  or MCP therefore appears within one poll, with no push channel needed.
  The app's own edits update at once from the edit response.
- **Chat edits.** Header edits go through the same dashboard tools and
  the same Chat mode rules (Confirm, Auto, Full auto) as Home edits today.
  Nothing new is added for the header.

## Constraints

- Use libraries: Mantine 9 (the Spotlight or Combobox family for the
  picker, notifications for Undo) and the icons the header already uses.
  Reordering uses `@dnd-kit` (sortable), a new dependency. The repo ships
  only `react-grid-layout`, which is a grid, not a sortable row, and a
  hand-rolled drag is ruled out. (My call.)
- Standard UX conventions only. Escape closes the picker. Enter adds the
  highlighted widget. Delete or Backspace on a focused tile does nothing.
  Removal goes through the menu.
- No legacy fallback: once the header renders from the spec, the
  hard-coded tiles and the fixed-id `stat()` reads are deleted.
- Every widget in the catalog has a Storybook story at header size, in
  light and dark, and the design-check suite passes. That includes a story
  with all catalog widgets, to show overflow.
- Tests prove the gap is fixed: a Go or UI test edits the header spec
  (adding `cpu`, removing `reclaim`), and the rendered header follows.
  That test fails on today's code.
- End to end on the built binary: an MCP or Chat dashboard edit to
  `header` changes the running app's header. Fake agent CLIs only.

## Left to the builder

- How tile behavior is attached to a panel (a small registry keyed by
  source and measure is the expected shape).
- The exact picker layout and grouping (Attention, This machine, Sessions,
  Hygiene, Fleet is a reasonable grouping).
- Whether "Show on Home" matches Home panels by id or by source and
  measure.
- How Undo is wired (restore the previous version, or apply the inverse
  edit).
- Icons per catalog widget.
