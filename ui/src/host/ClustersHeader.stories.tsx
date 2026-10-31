import { Box } from "@mantine/core";
import { spotlight } from "@mantine/spotlight";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor, within } from "storybook/test";
import { fakeService } from "../fixtures/service";
import { fragments } from "../fixtures/fragments";
import { value as format } from "../panels/format";
import { App } from "./App";
import { ClustersHeader } from "./ClustersHeader";
import { allClear, allWidgets, busy, fresh, headerCatalog, hot, manyMachines, oneStuck, widgetData } from "./Header-fixtures";

const headerViewports = { headerWide: { name: "Header 1280", styles: { width: "1280px", height: "900px" } }, headerNarrow: { name: "Header 640", styles: { width: "640px", height: "900px" } } };
const meta = {
  id: "header-clusters", title: "Components/Header", component: ClustersHeader,
  args: { data: busy, filter: null, chatOpen: true, onFilter: fn(), onOpenFleet: fn(), onSearch: fn(), onToggleChat: fn(), onShow: fn(), onAsk: fn() },
  decorators: [(Story, context) => context.parameters.fullApp ? <Story /> : <Box h={56} px="md" data-testid="header-shell" style={{ background: "var(--driga-ground)", borderBottom: "1px solid var(--driga-border)" }}><Story /></Box>],
  parameters: { layout: "fullscreen", viewport: { options: headerViewports }, docs: { story: { inline: false, iframeHeight: 120 }, description: { component: "Stored stat and gauge panels in spec order, in one 56 px row. Trailing widgets remain live in +N. Zero counts stay visible; unknown readings show an em dash. Menus act on the panel wherever it appears." } } },
} satisfies Meta<typeof ClustersHeader>;
export default meta;
type Story = StoryObj<typeof meta>;

async function ready(root: HTMLElement) {
  await waitFor(() => expect(root.querySelector("[data-header-ready=true]")).toBeTruthy());
}
/** Find a stored tile in the row or its live overflow menu. */
async function tile(root: HTMLElement, id: string): Promise<HTMLElement> {
  await ready(root);
  const selector = `[data-header-tile="${id}"] .w-tile`;
  let found = root.querySelector<HTMLElement>(selector);
  if (!found) {
    const more = within(root).getByRole("button", { name: /^\+\d+$/ });
    if (more.getAttribute("aria-expanded") !== "true") await userEvent.click(more);
    const menu = await screen.findByRole("menu");
    await waitFor(() => expect(menu).toBeVisible());
    found = menu.querySelector<HTMLElement>(selector);
  }
  // Mounting precedes the dropdown's fade-in. Assert its visible state,
  // rather than accepting a hidden item or racing the opening transition.
  await waitFor(() => expect(found).toBeVisible());
  return found!;
}
async function rest(root: HTMLElement) {
  await userEvent.keyboard("{Escape}");
  await userEvent.pointer({ target: document.body });
  (document.activeElement as HTMLElement | null)?.blur();
  await new Promise(resolve => setTimeout(resolve, 400));
  await waitFor(() => expect(screen.queryAllByRole("tooltip")).toHaveLength(0));
  await ready(root);
}
function geometry(root: HTMLElement) {
  const row = root.querySelector<HTMLElement>("[data-header-row]")!;
  const bounds = row.getBoundingClientRect();
  expect(bounds.height).toBeLessThanOrEqual(56);
  expect(row.scrollHeight).toBeLessThanOrEqual(56);
  const children = [...row.children].filter(el => el.checkVisibility());
  for (const child of children) {
    const rect = child.getBoundingClientRect();
    expect(rect.top).toBeGreaterThanOrEqual(bounds.top - 1);
    expect(rect.bottom).toBeLessThanOrEqual(bounds.bottom + 1);
  }
  for (let i = 1; i < children.length; i++) {
    expect(children[i - 1].getBoundingClientRect().right).toBeLessThanOrEqual(children[i].getBoundingClientRect().left + 1);
  }
  const more = within(root).queryByRole("button", { name: /^\+\d+$/ });
  if (more) expect(more.getBoundingClientRect().right).toBeLessThanOrEqual(more.parentElement!.parentElement!.getBoundingClientRect().right + 1);
  const slots = [...row.querySelectorAll<HTMLElement>("[data-header-tile]")];
  for (const slot of slots) {
    const rect = slot.getBoundingClientRect();
    expect(rect.top).toBeGreaterThanOrEqual(bounds.top - 1);
    expect(rect.bottom).toBeLessThanOrEqual(bounds.bottom + 1);
    expect(rect.right).toBeLessThanOrEqual(slot.parentElement!.parentElement!.getBoundingClientRect().right + 1);
    expect(rect.right).toBeLessThanOrEqual(within(root).getByRole("button", { name: /^Fleet:/ }).getBoundingClientRect().left);
  }
  // Labels and values share one line across the row, whether or not a tile
  // has a bar under its value.
  const stacks = slots.map(slot => slot.querySelector<HTMLElement>(".w-tile > .mantine-Stack-root")).filter(stack => stack?.checkVisibility()) as HTMLElement[];
  for (const [i, part] of [[0, "label"], [1, "value"]] as const) {
    const tops = stacks.map(stack => (stack.children[i] as HTMLElement).getBoundingClientRect().top);
    expect(Math.max(...tops) - Math.min(...tops), `${part} tops ${tops}`).toBeLessThanOrEqual(1);
  }
}

export const Busy: Story = {
  play: async ({ canvas, args, canvasElement }) => {
    const need = await tile(canvasElement, "waiting");
    await expect(need).toHaveTextContent(/Need you\s*3/);
    await expect(need).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(need); await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await expect(await tile(canvasElement, "finished")).toHaveTextContent(/To review\s*10/);
    await rest(canvasElement);
    await expect(await tile(canvasElement, "stuck")).toHaveTextContent(/Stuck\s*0/);
    await rest(canvasElement);
    const memory = await tile(canvasElement, "memory");
    await expect(memory).toHaveTextContent(/Memory · studio\s*24.*32\s*GB/);
    await expect(within(memory).getByRole("progressbar", { name: /75%/ })).toBeVisible();
    await userEvent.click(memory); await expect(args.onShow).toHaveBeenCalledWith(expect.objectContaining({ id: "memory", source: "local_activity", measure: "max:mem_used_bytes" }));
    await rest(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^Fleet: 4 machines/ })); await expect(args.onOpenFleet).toHaveBeenCalled();
    await userEvent.unhover(canvas.getByRole("button", { name: /^Fleet: 4 machines/ }));
    await rest(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Chat" })); await expect(args.onToggleChat).toHaveBeenCalled();
    await userEvent.unhover(canvas.getByRole("button", { name: "Chat" }));
    await rest(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Search" })); await expect(args.onSearch).toHaveBeenCalled();
    await rest(canvasElement);
  },
};
export const TileMenu: Story = {
  play: async ({ args, canvasElement }) => {
    const review = await tile(canvasElement, "finished");
    await userEvent.click(within(review.parentElement!).getByRole("button", { name: "More for To review" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Show on Home" }));
    await expect(args.onShow).toHaveBeenCalledWith(expect.objectContaining({ id: "finished", measure: "sum:to_review" }));
    await rest(canvasElement);
    await userEvent.pointer({ keys: "[MouseRight]", target: await tile(canvasElement, "memory") });
    await userEvent.click(await screen.findByRole("menuitem", { name: "Ask Chat about it" }));
    await expect(args.onAsk).toHaveBeenCalledWith("Memory");
    await rest(canvasElement);
  },
};
export const AllClear: Story = {
  args: { data: allClear },
  parameters: { docs: { description: { story: "Zero attention counts remain stored, visible and clickable. No all-clear substitution removes widgets." } } },
  play: async ({ canvas, canvasElement }) => {
    for (const [id, label] of [["waiting", "Need you"], ["finished", "To review"], ["stuck", "Stuck"]]) {
      await expect(await tile(canvasElement, id)).toHaveTextContent(new RegExp(`${label}\\s*0`));
      await rest(canvasElement);
    }
    await expect(canvas.getByRole("button", { name: /^Fleet: 3 machines · all working or idle/ })).toBeVisible();
    await expect(await tile(canvasElement, "leftover")).toHaveTextContent("· 0 trees");
    await rest(canvasElement);
  },
};
export const OneStuck: Story = {
  args: { data: oneStuck },
  play: async ({ args, canvasElement }) => {
    const stuck = await tile(canvasElement, "stuck");
    await expect(stuck).toHaveTextContent(/Stuck\s*1/);
    await expect(stuck).toHaveAttribute("data-attention", "danger");
    await userEvent.click(stuck); await expect(args.onFilter).toHaveBeenCalledWith("stuck");
    await expect(await tile(canvasElement, "waiting")).toHaveTextContent(/Need you\s*0/);
    await rest(canvasElement);
  },
};
export const Hot: Story = {
  name: "Memory past 90%", args: { data: hot },
  play: async ({ canvasElement }) => {
    const memory = await tile(canvasElement, "memory");
    await expect(within(memory).getByRole("progressbar", { name: /94%/ })).toBeVisible();
    await expect(memory).toHaveTextContent(/30.*32\s*GB/);
    await rest(canvasElement);
  },
};
export const FirstLoad: Story = {
  args: { data: fresh },
  play: async ({ canvas, canvasElement }) => {
    await expect(await tile(canvasElement, "leftover")).toHaveTextContent(/Leftover\s*1\.8\s*GB\s*· 2 trees/);
    await expect(canvas.getByRole("button", { name: /^Fleet: 0 machines/ })).toBeVisible();
    await rest(canvasElement);
  },
};
export const Loading: Story = {
  args: { data: null },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("img", { name: "Driga" })).toBeVisible();
    await expect(canvas.queryByRole("button", { name: /^Need you/ })).toBeNull();
    await expect(canvas.getByRole("button", { name: "Chat" })).toBeVisible();
  },
};
export const ManyMachines: Story = {
  args: { data: manyMachines },
  play: async ({ canvas, canvasElement }) => {
    await ready(canvasElement);
    await expect(canvas.getByRole("button", { name: /^Fleet: 10 machines/ })).toHaveTextContent("10");
  },
};
export const LaptopWidth: Story = {
  globals: { viewport: { value: "laptop", isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    await ready(canvasElement);
    await expect(canvas.getByRole("button", { name: /^\+\d+$/ })).toBeVisible();
    await expect(await tile(canvasElement, "reclaim")).toHaveTextContent(/Reclaimable\s*6.1\s*GB/);
    await rest(canvasElement);
    await expect(await tile(canvasElement, "leftover")).toHaveTextContent(/Leftover\s*1\.8\s*GB\s*· 2 trees/);
    await rest(canvasElement); geometry(canvasElement);
  },
};
export const TallFontMetrics: Story = {
  beforeEach: async () => {
    const face = new FontFace("Catalog tall mono", 'url("/static/fonts/ibm-plex-mono-latin-600-normal.woff2")', { weight: "400 700", ascentOverride: "120%", descentOverride: "40%", lineGapOverride: "0%" });
    await face.load(); document.fonts.add(face);
    return () => { document.fonts.delete(face); };
  },
  decorators: [Story => <Box h="100%" style={{ "--mantine-font-family-monospace": '"Catalog tall mono", monospace' }}><Story /></Box>],
  play: async ({ canvasElement }) => {
    await expect(await tile(canvasElement, "memory")).toHaveTextContent(/Memory · studio/);
    await rest(canvasElement); geometry(canvasElement);
  },
};
export const Filtering: Story = {
  args: { filter: "waiting" },
  play: async ({ canvasElement }) => {
    await expect(await tile(canvasElement, "waiting")).toHaveAttribute("aria-pressed", "true");
    await expect(await tile(canvasElement, "finished")).toHaveAttribute("aria-pressed", "false");
    await rest(canvasElement);
  },
};
export const KeyboardFocus: Story = {
  play: async ({ args, canvasElement }) => {
    await ready(canvasElement); await userEvent.tab();
    const need = await tile(canvasElement, "waiting");
    await expect(need).toHaveFocus();
    await expect(await screen.findByRole("tooltip")).toHaveTextContent(/up 2 in the last hour/);
    await userEvent.keyboard("{Enter}"); await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await rest(canvasElement);
  },
};

/** Each catalog entry uses the same header-size renderer and engine result. */
function widget(id: string): Story {
  const panel = headerCatalog.find(p => p.id === id)!;
  return { args: { data: widgetData(id) }, play: async ({ canvasElement }) => {
    const rendered = await tile(canvasElement, id);
    await expect(rendered).toHaveAttribute("data-size", "header");
    await expect(rendered).toHaveTextContent(panel.title);
    const reading = allWidgets.tiles.find(t => t.id === id)!;
    expect(reading.value).not.toBeNull();
    const number = format(reading.value!, reading.unit).split(" ")[0];
    // A gauge shows its share inside the ring and names the full reading for assistive tech.
    if (panel.viz === "gauge") await expect(within(rendered).getByRole("img")).toHaveAccessibleName(new RegExp(number));
    else await expect(rendered).toHaveTextContent(number);
    if (reading.total != null) await expect(rendered).toHaveTextContent(`/ ${format(reading.total, reading.unit).split(" ")[0]}`);
    if (reading.caption != null) await expect(rendered).toHaveTextContent(`· ${reading.caption} trees`);
    geometry(canvasElement);
  } };
}
export const Waiting = widget("waiting");
export const Finished = widget("finished");
export const Stuck = widget("stuck");
export const Leftover = widget("leftover");
export const LeftoverOne: Story = {
  ...widget("leftover"),
  args: { data: { ...widgetData("leftover"), tiles: widgetData("leftover").tiles.map(t => ({ ...t, caption: 1 })) } },
  play: async ({ canvasElement }) => {
    await expect(await tile(canvasElement, "leftover")).toHaveTextContent(/· 1 tree$/);
    geometry(canvasElement);
  },
};
export const Reclaim = widget("reclaim");
export const Memory: Story = {
  ...widget("memory"),
  parameters: { docs: { description: { story: "Memory keeps its total and pressure bar in 56 px; the redundant trend and delta are omitted." } } },
  play: async context => {
    await widget("memory").play!(context);
    const memory = await tile(context.canvasElement, "memory");
    await expect(within(memory).getByRole("progressbar")).toBeVisible();
    expect(memory.querySelector('[role="img"]')).toBeNull();
    expect(memory.textContent).not.toContain("1h");
    geometry(context.canvasElement);
  },
};
/** Tiles with and without a bar under the value, side by side as in the
 * screenshot that found them misaligned: labels and values share one line. */
export const MixedAlignment: Story = {
  args: { data: { ...allWidgets, tiles: ["finished", "leftover", "reclaim", "memory", "cpu", "disk"].map(id => allWidgets.tiles.find(t => t.id === id)!) } },
  globals: { viewport: { value: "headerWide", isRotated: false } },
  play: async ({ canvasElement }) => {
    await ready(canvasElement);
    for (const id of ["memory", "disk", "leftover"]) await expect(await tile(canvasElement, id)).toBeVisible();
    geometry(canvasElement);
  },
};
export const CPU = widget("cpu");
export const Disk = widget("disk");
export const Load = widget("load");
export const Swap = widget("swap");
export const Events = widget("events");
export const Agents = widget("agents");
export const Working = widget("working");
export const WaitingNow = widget("waiting_now");
export const Inbox = widget("inbox");
export const Orphans = widget("orphans");
export const MachinesUp = widget("machines_up");
export const AgentMemory = widget("agent_memory");

export const AllWidgets: Story = {
  args: { data: allWidgets }, globals: { viewport: { value: "headerWide", isRotated: false } },
  parameters: { docs: { description: { story: "All 18 catalog entries stress overflow (stored headers allow 16). This story owns its 1280 px viewport; its companion owns 640 px." } } },
  play: async ({ canvas, canvasElement, args, globals }) => {
    expect(window.innerWidth).toBe(globals.viewport.value === "headerNarrow" ? 640 : 1280);
    await ready(canvasElement); geometry(canvasElement);
    const more = canvas.getByRole("button", { name: /^\+\d+$/ });
    await expect(more).toBeVisible();
    const hidden = Number(more.textContent?.slice(1));
    const visible = canvasElement.querySelectorAll("[data-header-tile]").length;
    expect(hidden + visible).toBe(18);
    await userEvent.click(more);
    const menu = await screen.findByRole("menu");
    expect(menu.querySelectorAll("[data-header-tile]").length).toBe(hidden);
    const last = within(menu).getByRole("menuitem", { name: /Agent memory/ });
    await expect(last).toHaveTextContent(/GB/);
    await userEvent.click(last);
    await expect(args.onShow).toHaveBeenCalledWith(expect.objectContaining({ id: "agent_memory" }));
    await rest(canvasElement);
  },
};
export const AllWidgetsNarrow: Story = { ...AllWidgets, globals: { viewport: { value: "headerNarrow", isRotated: false } } };

const editingRequests = fn();
export const Editing: Story = {
  render: () => <App />,
  globals: { viewport: { value: "headerWide", isRotated: false } },
  parameters: { fullApp: true, makeService: () => {
    const header = structuredClone(fragments.header);
    header.dashboard.panels = header.dashboard.panels.filter(p => ["waiting", "reclaim", "memory"].includes(p.id));
    return fakeService({ open: { header }, onRequest: editingRequests });
  }, docs: { story: { iframeHeight: 900 } } },
  beforeEach: () => { editingRequests.mockClear(); spotlight.close(); history.replaceState(null, "", "/app/"); return () => spotlight.close(); },
  play: async ({ canvas, canvasElement }) => {
    // App supplies the editor and versioned mutations through the fake service.
    const add = await canvas.findByRole("combobox", { name: "Add widget" });
    await expect(add).toBeVisible();
    const reclaim = await tile(canvasElement, "reclaim");
    await userEvent.click(within(reclaim.parentElement!).getByRole("button", { name: "More for Reclaimable" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Remove from header" }));
    await waitFor(() => expect(editingRequests).toHaveBeenCalledWith("/app/api/edit", expect.objectContaining({ base_version: 0, operations: [{ op: "remove", id: "reclaim" }] })));
    await waitFor(() => expect(canvasElement.querySelector('[data-header-tile="reclaim"]')).toBeNull());
    await userEvent.click(canvas.getByRole("combobox", { name: "Add widget" }));
    const search = await screen.findByRole("textbox", { name: "Find a widget" });
    await userEvent.type(search, "CPU");
    await expect(await screen.findByRole("option", { name: "CPU" })).toBeVisible();
    await userEvent.keyboard("{Enter}");
    await expect(await tile(canvasElement, "cpu")).toHaveTextContent(/CPU/);
    await expect(editingRequests).toHaveBeenCalledWith("/app/api/edit", expect.objectContaining({ base_version: 1, operations: [{ op: "add", panel: headerCatalog.find(p => p.id === "cpu") }] }));
    await rest(canvasElement);
    const handle = canvas.getByRole("button", { name: "Reorder Memory" });
    handle.focus(); await userEvent.keyboard("[Space][ArrowLeft][Space]");
    await waitFor(() => expect(editingRequests).toHaveBeenCalledWith("/app/api/edit", expect.objectContaining({ base_version: 2, operations: [{ op: "move", id: "memory", before: "waiting" }] })));
    await waitFor(() => expect(canvasElement.querySelector("[data-header-tile]")).toHaveAttribute("data-header-tile", "memory"));
    const undo = screen.getAllByRole("button", { name: "Undo" }).at(-1)!;
    await userEvent.click(undo);
    await waitFor(() => expect(editingRequests).toHaveBeenCalledWith("/app/api/replace", expect.objectContaining({ base_version: 3 })));
    await waitFor(() => expect(canvasElement.querySelector("[data-header-tile]")).toHaveAttribute("data-header-tile", "waiting"));
    await expect(await tile(canvasElement, "cpu")).toBeVisible();
    const restored = await screen.findByText("Header restored");
    await waitFor(() => expect(restored).toBeVisible());
    // Undoing the move leaves the earlier remove/add toasts and creates a
    // restoration toast. Close all of them before checking the page layout.
    for (const dismiss of screen.getAllByRole("button", { name: "Dismiss" })) await userEvent.click(dismiss);
    await waitFor(() => expect(screen.queryAllByRole("alert")).toHaveLength(0));
    await rest(canvasElement);
  },
};

export const DefaultLaptop: Story = {
  render: () => <App />,
  globals: { viewport: { value: "laptop", isRotated: false } },
  parameters: { fullApp: true, makeService: () => {
    const header = structuredClone(fragments.header_quiet);
    // A quiet inbox still has a live local memory reading, as in the app screenshots.
    header.results = header.results.map(r => r.id === "memory" ? structuredClone(fragments.header.results.find(r => r.id === "memory")!) : r);
    return fakeService({ open: { header } });
  }, docs: { story: { iframeHeight: 768 }, description: { story: "The default six tiles, editor, fleet, Search and open Chat fit together at 1024 px." } } },
  beforeEach: () => { spotlight.close(); history.replaceState(null, "", "/app/"); return () => spotlight.close(); },
  play: async ({ canvas, canvasElement }) => {
    expect(window.innerWidth).toBe(1024);
    await canvas.findByRole("combobox", { name: "Add widget" });
    await ready(canvasElement);
    expect([...canvasElement.querySelectorAll("[data-header-tile]")].map(el => el.getAttribute("data-header-tile")))
      .toEqual(["waiting", "finished", "stuck", "leftover", "reclaim", "memory"]);
    expect(canvas.queryByRole("button", { name: /^\+\d+$/ })).toBeNull();
    await expect(canvas.getByRole("button", { name: "Header options" })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Fleet:/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: "Search" })).toBeVisible();
    await expect(canvas.getByRole("button", { name: "Chat" })).toHaveAttribute("aria-pressed", "true");
    await expect(canvas.getByRole("button", { name: "Close chat" })).toBeVisible();
    await expect(await tile(canvasElement, "leftover")).toHaveTextContent(/0\s*B\s*· 0 trees/);
    await expect(within(await tile(canvasElement, "memory")).getByRole("progressbar")).toBeVisible();
    await rest(canvasElement); geometry(canvasElement);
  },
};

export const PointerReorder: Story = {
  args: { data: { ...allClear, tiles: allClear.tiles.slice(0, 3) }, onMove: fn() },
  play: async ({ canvas, canvasElement, args }) => {
    const need = await tile(canvasElement, "waiting");
    const review = await tile(canvasElement, "finished");
    await userEvent.click(need);
    await expect(args.onFilter).toHaveBeenCalledTimes(1);
    await expect(args.onMove).not.toHaveBeenCalled();
    const handle = canvas.getByRole("button", { name: "Reorder Need you" });
    expect(getComputedStyle(handle).opacity).toBe("0");
    handle.focus();
    expect(getComputedStyle(handle).opacity).toBe("1");
    handle.blur();
    const from = need.getBoundingClientRect(), to = review.getBoundingClientRect();
    const x = from.x + from.width / 2, y = from.y + from.height / 2;
    await userEvent.pointer([
      { target: need, keys: "[MouseLeft>]", coords: { x, y } },
      { target: need, coords: { x: x + 8, y } },
      { target: review, coords: { x: to.x + to.width / 2, y } },
      { keys: "[/MouseLeft]" },
    ]);
    await expect(args.onMove).toHaveBeenCalledWith("waiting", "stuck");
    await expect(args.onFilter).toHaveBeenCalledTimes(1);
    await userEvent.unhover(review);
    await rest(canvasElement); geometry(canvasElement);
  },
};
