import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor } from "storybook/test";
import { ClustersHeader } from "./ClustersHeader";
import { allClear, busy, fresh, hot, manyMachines, oneStuck } from "./Header-fixtures";

/** The app's header row: four clusters set apart by hairlines, inside the shell. */
const meta = {
  id: "header-clusters",
  title: "Components/Header",
  component: ClustersHeader,
  args: { data: busy, filter: null, chatOpen: true, onFilter: fn(), onOpenFleet: fn(), onSearch: fn(), onToggleChat: fn(), onShow: fn(), onAsk: fn() },
  // The row as the shell holds it: 56 px on the translucent ground.
  decorators: [Story => <Box h={56} px="md" data-testid="header-shell" style={{ background: "var(--driga-ground)", borderBottom: "1px solid var(--driga-border)" }}><Story /></Box>],
  parameters: {
    layout: "fullscreen",
    docs: {
      story: { inline: false, iframeHeight: 120 },
      description: {
        component: [
          "The header as four clusters set apart by hairlines: attention (Need you, To review, Stuck), hygiene (Leftover, Reclaimable), this machine's memory with a sparkline, a delta chip and a 3 px pressure bar, and one dot per machine in the fleet. Then search with ⌘K and the Chat toggle. Every tile is the KPI tile at its header size, with the tile menu (Show on Home, Ask Chat) in its corner and on right-click.",
          "A count that needs you takes its tone: its tint, a 2 px left rule and a dot before the number; only the waiting dot pulses, and not under reduced motion. Zeros stay quiet. When all three are zero the tiles fold into one line, \"Nothing needs you · checked HH:MM\". A tile's tooltip names its filter, the hour's change in words and its key: 1, 2 and 3 focus the tiles; g then f opens the Fleet. The memory bar stays the accent until 90%, then turns red.",
          "Below 1200 px the hygiene cluster folds into one Reclaim tile, the memory cluster drops its delta chip, the fleet dots fold into a count with the worst state's dot, and search keeps only its icon and key.",
        ].join("\n\n"),
      },
    },
  },
} satisfies Meta<typeof ClustersHeader>;
export default meta;
type Story = StoryObj<typeof meta>;

/** rest leaves the row as a reviewer should see it: nothing hovered or focused. */
async function rest(canvasRoot: HTMLElement) {
  // The pointer leaves every control it may have crossed, and focus with it.
  for (const control of canvasRoot.querySelectorAll<HTMLElement>("button, [role=img]")) await userEvent.unhover(control);
  (document.activeElement as HTMLElement | null)?.blur();
  // A tooltip opens after the theme's 300 ms delay, so one the last click
  // started can appear after an immediate check; wait the delay out first.
  await new Promise(resolve => setTimeout(resolve, 400));
  await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
}

export const Busy: Story = {
  parameters: { docs: { description: { story: "Three agents wait on you, three turns await review, nothing is stuck; two leftover process trees; memory at 75% and falling; one of four machines unreachable (the hollow dot). Click a tile to filter the inbox to its kind." } } },
  play: async ({ canvas, args , canvasElement }) => {
    const needYou = canvas.getByRole("button", { name: /^Need you\s*3$/ });
    await expect(needYou).toBeVisible();
    await expect(needYou).toHaveAttribute("aria-pressed", "false");
    await expect(canvas.getByRole("button", { name: /^To review\s*10$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Stuck\s*0$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Memory · studio\s*24.*32\s*GB/ })).toBeVisible();
    await expect(canvas.getByRole("progressbar", { name: /75%/ })).toBeInTheDocument();
    await expect(canvas.getByRole("button", { name: /^Fleet: 4 machines · studio waiting · build-01 stuck · build-02 unreachable/ })).toBeVisible();
    await userEvent.click(needYou);
    await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await userEvent.click(canvas.getByRole("button", { name: /^Fleet: 4 machines/ }));
    await expect(args.onOpenFleet).toHaveBeenCalled();
    await userEvent.click(canvas.getByRole("button", { name: /^Memory · studio/ }));
    await expect(args.onShow).toHaveBeenCalledWith("memory");
    await userEvent.click(canvas.getByRole("button", { name: "Chat" }));
    await expect(args.onToggleChat).toHaveBeenCalled();
    await userEvent.click(canvas.getByRole("button", { name: "Search" }));
    await expect(args.onSearch).toHaveBeenCalled();
    await rest(canvasElement);
  },
};

export const TileMenu: Story = {
  name: "Tile menu",
  parameters: { docs: { description: { story: "Every tile keeps its menu: Show on Home and Ask Chat about it, behind the ⋯ in its corner or on right-click." } } },
  play: async ({ canvas, args , canvasElement }) => {
    await userEvent.click(canvas.getByRole("button", { name: "More for To review" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Show on Home" }));
    await expect(args.onShow).toHaveBeenCalledWith("inbox");
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    await userEvent.pointer({ keys: "[MouseRight]", target: canvas.getByRole("button", { name: /^Memory · studio/ }) });
    await userEvent.click(await screen.findByRole("menuitem", { name: "Ask Chat about it" }));
    await expect(args.onAsk).toHaveBeenCalledWith("Memory");
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    await rest(canvasElement);
  },
};

export const AllClear: Story = {
  name: "All clear",
  args: { data: allClear },
  parameters: { docs: { description: { story: "Every attention count is zero, so the three tiles fold into one line with the time of the last check. Hygiene zeros stay quiet." } } },
  play: async ({ canvas }) => {
    const status = canvas.getByRole("status");
    await expect(status).toHaveTextContent(/Nothing needs you/);
    await expect(status).toHaveTextContent(/checked \d{1,2}:\d{2}/);
    await expect(canvas.queryByRole("button", { name: /^Need you/ })).toBeNull();
    await expect(canvas.getByRole("button", { name: /^Fleet: 3 machines · all working or idle/ })).toBeVisible();
  },
};

export const OneStuck: Story = {
  name: "One stuck",
  args: { data: oneStuck },
  parameters: { docs: { description: { story: "One session has gone quiet for too long: only the Stuck tile takes its tone; Need you and To review are quiet zeros, and the tiles do not fold." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Stuck\s*1$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Need you\s*0$/ })).toBeVisible();
    await expect(canvas.queryByRole("status")).toBeNull();
    await expect(canvas.getByRole("button", { name: /build-01 stuck/ })).toBeVisible();
  },
};

export const Hot: Story = {
  name: "Memory past 90%",
  args: { data: hot },
  parameters: { docs: { description: { story: "The memory bar is the accent until the danger threshold and red from it; there is no amber step, since a dev machine sits at 70–80% all day." } } },
  play: async ({ canvas }) => { await expect(canvas.getByRole("progressbar", { name: /94%/ })).toBeInTheDocument(); },
};

export const FirstLoad: Story = {
  name: "First load",
  args: { data: fresh },
  globals: { viewport: { value: "desktop", isRotated: false } },
  parameters: { docs: { description: { story: "Before the fleet or any kept reading: the counts without a change, the leftover bytes without a count, no dots." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Leftover\s*2\s*GB/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Fleet: 0 machines/ })).toBeVisible();
  },
};

export const Loading: Story = {
  args: { data: null },
  parameters: { docs: { description: { story: "Until the first load the header shows its mark, search and Chat only." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("img", { name: "Driga" })).toBeVisible();
    await expect(canvas.queryByRole("button", { name: /^Need you/ })).toBeNull();
    await expect(canvas.getByRole("button", { name: "Chat" })).toBeVisible();
  },
};

export const ManyMachines: Story = {
  name: "Many machines",
  args: { data: manyMachines },
  parameters: { docs: { description: { story: "Ten machines: the fleet pulse shows eight dots and +2. Hover a dot for the machine's line; click anywhere on the pulse to open the Fleet." } } },
  play: async ({ canvas }) => {
    const fleet = canvas.getByRole("button", { name: /^Fleet: 10 machines/ });
    await expect(fleet).toBeVisible();
    await expect(fleet).toHaveTextContent(/\+2|10/);
  },
};

export const LaptopWidth: Story = {
  name: "Laptop width",
  globals: { viewport: { value: "laptop", isRotated: false } },
  parameters: { docs: { description: { story: "At 1024 px: one Reclaim tile stands for both hygiene counts, the memory cluster keeps its sparkline but drops the delta chip, the fleet folds into a count, and search keeps only its icon and key." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Reclaim\s*2\s*· 6.1 GB leftover process trees, 1.8 GB; 6.1 GB reclaimable/ })).toBeVisible();
    await expect(canvas.queryByRole("button", { name: /^Reclaimable/ })).toBeNull();
    const fleet = canvas.getByRole("button", { name: /^Fleet: 4 machines/ });
    await expect(fleet).toBeVisible();
    await expect(fleet).toHaveTextContent("4");
    await expect(canvas.getByRole("button", { name: /^Need you\s*3$/ })).toBeVisible();
  },
};

/** Match Linux's taller Plex Mono bounds on every review platform. */
export const TallFontMetrics: Story = {
  name: "Tall font metrics",
  beforeEach: async () => {
    const face = new FontFace("Catalog tall mono", 'url("/static/fonts/ibm-plex-mono-latin-600-normal.woff2")', {
      weight: "400 700", ascentOverride: "120%", descentOverride: "40%", lineGapOverride: "0%",
    });
    await face.load();
    document.fonts.add(face);
    return () => { document.fonts.delete(face); };
  },
  decorators: [Story => <Box h="100%" style={{ "--mantine-font-family-monospace": '"Catalog tall mono", monospace' }}><Story /></Box>],
  parameters: { docs: { description: { story: "Linux reports taller Plex Mono text bounds. The labels and values keep their space, including the memory machine name, while every tile fits inside the 56 px shell." } } },
  play: async ({ canvas, canvasElement }) => {
    await expect(canvas.getByRole("button", { name: /^Memory · studio/ })).toBeVisible();
    const shell = canvas.getByTestId("header-shell").getBoundingClientRect();
    for (const tile of canvasElement.querySelectorAll<HTMLElement>(".w-tile")) {
      if (!tile.checkVisibility()) continue;
      const bounds = tile.getBoundingClientRect();
      expect(bounds.top).toBeGreaterThanOrEqual(shell.top);
      expect(bounds.bottom).toBeLessThanOrEqual(shell.bottom);
    }
  },
};

export const Filtering: Story = {
  args: { filter: "waiting" },
  parameters: { docs: { description: { story: "The inbox is filtered to what needs you: the tile is pressed, with an inset ring in the accent." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Need you\s*3$/ })).toHaveAttribute("aria-pressed", "true");
    await expect(canvas.getByRole("button", { name: /^To review\s*10$/ })).toHaveAttribute("aria-pressed", "false");
  },
};

export const KeyboardFocus: Story = {
  name: "Keyboard focus",
  parameters: { docs: { description: { story: "Tab reaches the first tile, with the accent focus ring and a tooltip naming its filter, the hour's change and its key. Enter filters. The keys themselves are the app's, beside ⌘K." } } },
  play: async ({ canvas, args , canvasElement }) => {
    await userEvent.tab();
    const needYou = canvas.getByRole("button", { name: /^Need you\s*3$/ });
    await expect(needYou).toHaveFocus();
    await expect(await screen.findByRole("tooltip")).toHaveTextContent(/up 2 in the last hour/);
    await userEvent.keyboard("{Enter}");
    await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await rest(canvasElement);
  },
};
