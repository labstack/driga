# Flow — Final v2

Flow monitors stored agent work and resolves interruptions. The approved Final v2
Map and Timeline replace the previous timeline board and family signal map.

## Navigation and ownership

Both interfaces show three destinations: **0 Flow**, **1 Projects**, **2 Machines**.
Flow opens on Map. The web segmented control and TUI `m` / `t` / `s` select Map,
Timeline and Sessions. In the terminal, Map uses the same project tree as Timeline.
`/` finds, arrows or `j`/`k` select, Enter opens context, and Esc returns. `M` changes
TUI machine scope. Sessions retains the existing sortable, searchable directory;
`S` sorts in the TUI. Projects owns repository/check-out evidence. Machines owns
health, process review and disk reclamation (`h` / `p` / `d` in the TUI).

Chat is a contextual panel on `c`, with the current session, project and machine.
Esc closes it and retains the draft and origin. Wide terminals keep the destination
beside Chat; narrow terminals use the available reading width for Chat. CLI command
names and schema-2 session/family APIs are unchanged.

Notification and retained event history live in Flow's expandable Activity section;
TUI `h` opens history. A completed turn is history, not a live wait or a session end.

## Shared model and honest evidence

`LoadBoardSnapshot` loads sessions, events, observation minutes, inbox spans and
session files in bulk for each scoped machine. `Board` projects Map and Timeline in
one pass, using a fixed last-hour display window. There is no per-session query loop.
Live waits, pending permissions and the state verdict produce one Needs you list,
oldest first; its length equals the waiting state count. Session identity includes
machine identity. Ended sessions cannot be revived by an old open inbox span.

Only stored `ParentID` establishes a spawn edge. Same-file overlap requires the
same full stored path on the same machine, with both sessions still live. Equal
basenames and equal paths on different hosts do not establish overlap. A shared
file uses a deterministic star, avoiding quadratic all-pairs edges.

Historical working slices require measured transcript/CPU activity or real tool
telemetry. A current verdict applies only at the right edge. Unknown history remains
neutral; recorded waits are amber and stalled state caps are hatched. Tool ticks
and counts require Claude `otlp:tool_result` or Copilot `postToolUse` events and are
labelled **telemetry**. Codex/Gemini observation growth never becomes a tool tick.
Cost appears only with stored cost metadata and is labelled **retained window**.
Missing usage, tools, files and agent badges are omitted. No handoff edges,
particles, streams, Sankey, replay or inferred outcomes are rendered.

## Map

The Go model produces SVG coordinates. Project/machine regions occupy a two-column
grid; height follows family/session count. Paired-machine regions are dashed. Leads
are ordered by start time and identity down the left column. Workers occupy the
adjacent column, with purple elbow spawn edges. Four workers are visible per family;
remaining workers fold into a `+N` control that opens Timeline. Timeline retains the
complete forest. Missing parents and cycles cannot hide sessions.

Nodes have radius 18, a state glyph, a small agent-letter badge and a title below.
Working is green `▶`, waiting amber `◆`, stalled red `‖`, idle blue `○`, and ended
muted `✓`. Stalled strokes are dashed; ended strokes are dotted and faded. Recent
stored activity controls fill opacity. Only waiting/stalled nodes show an age.
Waiting has a static amber ring; selection has a purple ring. Regions and coordinates
never depend on current activity or verdict, so refreshes do not move sessions.

Same-file links are red dashed lines with a filename pill. Nodes are focusable SVG
buttons with title/state/age/agent labels. Click and Enter select the shared inspector.
A legend explains glyphs, spawn, overlap, brightness and telemetry. The header shows
state counts, a small SVG events/minute sparkline and elapsed time since observation.
The empty state explains where the next session will appear.

## Timeline and inspector

Timeline is a project-grouped ARIA tree with roving tabindex and arrow-key navigation.
Rows show branch, state, title, agent, sixty minute slices, real telemetry ticks,
file-overlap markers and age. The selected family repeats the same evidence in a
compact tree in the inspector. The inspector conditionally shows now, files,
shared-file annotations, tools and retained-window cost.

Needs you stays above the family. The oldest item has a full wrapping command;
compact following rows and overflow preserve access to other waits. Allow/Deny use
the existing authenticated, CSRF-protected prompt-answer endpoint. A new permission
never takes focus. Buttons arm after 600 ms; terminal answers require explicit shelf
focus and the same review delay. Enter can open the full, scrollable prompt inspector.
Terminal uses the existing jump resolver; Open session uses the session inspector;
Ask Chat opens the panel with selected context. Process termination lives in Machines.

## Refresh and libraries

The console's existing SSE notifications trigger htmx fragment refreshes. Vendored
**Idiomorph 0.7.4**, including its htmx extension, supplies `morph:innerHTML` swaps;
its BSD Zero Clause license is recorded in `static/vendor/LICENSES.txt`. Stable DOM
IDs and client state preserve session identity, view, search, caret, keyboard focus
and scroll. No secondary JSON delta stream or client-rendered Map exists.

Lip Gloss v2 `tree` supplies terminal connectors, bubbles supplies the session table,
filter, text composer and inspectors, and the existing event-count spark renderer
supplies compact terminal history. No force-layout library or JS framework is added.
Motion is static by default and the CSS also honours reduced-motion preferences.

## Verification

Model tests cover input-order/activity-independent slots, worker folding at forty
sessions, complete Timeline membership, parent links, same-path scope, paired-region
styling, attention parity/order, unknown history and telemetry honesty. A 200-session
benchmark exercises the combined Map and Timeline model. Handler/CSP/escaping and
Node DOM tests cover refresh state, keyboard movement and prompt arming. A synthetic
loopback Playwright harness exercises the actual htmx/Idiomorph swap and captures
light/dark Map/Timeline and empty/quiet layouts. TUI goldens cover 80, 100, 140 and
200 columns, colour-independent geometry, and Windows newline normalization.
