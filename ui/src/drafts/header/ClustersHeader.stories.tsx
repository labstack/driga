import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent } from "storybook/test";
import { ClustersHeader } from "./ClustersHeader";
import { allClear, busy, manyMachines, oneStuck } from "./Header-fixtures";

/** Variant A of the header: four clusters set apart by hairlines. */
const meta = {
  id: "header-clusters",
  title: "Drafts/Header/Clusters with fleet pulse",
  component: ClustersHeader,
  args: { ...busy, filter: null, chatOpen: true, onFilter: fn(), onOpenFleet: fn(), onSearch: fn(), onToggleChat: fn(), onShow: fn() },
  parameters: {
    layout: "fullscreen",
    docs: {
      story: { inline: false, iframeHeight: 120 },
      description: {
        component: [
          "**Variant A: clusters with fleet pulse.** The header as four clusters set apart by hairlines: attention (Need you, To review, Stuck), hygiene (Leftover, Reclaimable), this machine's memory with a sparkline, a delta chip and a 3 px pressure bar, and one dot per machine in the fleet. Then search with ⌘K and the Chat toggle.",
          "A count that needs you takes its tone: a 12% tint, a 2 px left rule and a dot before the number; only the waiting dot pulses, and not under reduced motion. Zeros stay quiet. When all three are zero the tiles fold into one line, \"Nothing needs you · checked HH:MM\". The keys 1, 2 and 3 focus the attention tiles; g then f opens the Fleet.",
          "Below 1200 px the hygiene cluster folds into one Reclaim tile, the memory cluster drops its delta chip, the fleet dots fold into a count with the worst state's dot, and search keeps only its icon and key.",
          "Compare with *Attention bar with machine chips*, which ranks the three counts far above everything else and names machines instead of dotting them; and with *Compare › Today* for the header as it is now.",
        ].join("\n\n"),
      },
    },
  },
} satisfies Meta<typeof ClustersHeader>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Busy: Story = {
  parameters: { docs: { description: { story: "Three agents wait on you, ten turns await review, nothing is stuck; two leftover process trees; memory at 75% and falling; one of four machines unreachable (the hollow dot). Click a tile to filter the inbox to its kind." } } },
  play: async ({ canvas, args }) => {
    const needYou = canvas.getByRole("button", { name: /^Need you\s*3$/ });
    await expect(needYou).toBeVisible();
    await expect(needYou).toHaveAttribute("aria-pressed", "false");
    await expect(canvas.getByRole("button", { name: /^To review\s*10$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Stuck\s*0$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Memory · studio\s*24.*32\s*GB.*75%/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Fleet: 4 machines · studio waiting · build-01 stuck · build-02 unreachable/ })).toBeVisible();
    await userEvent.click(needYou);
    await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await userEvent.click(canvas.getByRole("button", { name: /^Fleet: 4 machines/ }));
    await expect(args.onOpenFleet).toHaveBeenCalled();
    await userEvent.click(canvas.getByRole("button", { name: /^Memory · studio/ }));
    await expect(args.onShow).toHaveBeenCalledWith("memory");
    await userEvent.click(canvas.getByRole("button", { name: "Chat" }));
    await expect(args.onToggleChat).toHaveBeenCalled();
    await userEvent.click(canvas.getByRole("button", { name: "Search sessions and dashboards" }));
    await expect(args.onSearch).toHaveBeenCalled();
  },
};

export const AllClear: Story = {
  name: "All clear",
  args: { ...allClear },
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
  args: { ...oneStuck },
  parameters: { docs: { description: { story: "One session has gone quiet for too long: only the Stuck tile takes its tone; Need you and To review are quiet zeros, and the tiles do not fold." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Stuck\s*1$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Need you\s*0$/ })).toBeVisible();
    await expect(canvas.queryByRole("status")).toBeNull();
    await expect(canvas.getByRole("button", { name: /build-01 stuck/ })).toBeVisible();
  },
};

export const ManyMachines: Story = {
  name: "Many machines",
  args: { ...manyMachines },
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
  parameters: { docs: { description: { story: "Tab reaches the first tile, with the accent focus ring and a tooltip naming its key. Enter filters. The keys 2 and 3 focus the other tiles; g then f opens the Fleet. Review on the canvas, where the Interactions panel replays it." } } },
  play: async ({ canvas, args }) => {
    await userEvent.tab();
    const needYou = canvas.getByRole("button", { name: /^Need you\s*3$/ });
    await expect(needYou).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await userEvent.keyboard("2");
    await expect(canvas.getByRole("button", { name: /^To review\s*10$/ })).toHaveFocus();
    await userEvent.keyboard("gf");
    await expect(args.onOpenFleet).toHaveBeenCalled();
  },
};
