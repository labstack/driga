import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent } from "storybook/test";
import { AttentionHeader } from "./AttentionHeader";
import { allClear, busy, manyMachines, oneStuck } from "./Header-fixtures";

/** Variant B of the header: an attention bar with machine chips. */
const meta = {
  id: "header-attention",
  title: "Drafts/Header/Attention bar with machine chips",
  component: AttentionHeader,
  args: { ...busy, filter: null, chatOpen: true, onFilter: fn(), onOpenFleet: fn(), onSearch: fn(), onToggleChat: fn(), onShow: fn() },
  parameters: {
    layout: "fullscreen",
    docs: {
      story: { inline: false, iframeHeight: 120 },
      description: {
        component: [
          "**Variant B: attention bar with machine chips.** Three large counts (Need you, To review, Stuck) with their last hour faded behind each number and a delta against an hour ago, tinted by what the change means: more stuck or more waiting is bad, more to review is neutral. Everything else is demoted to a quiet cluster on the right: leftover trees, reclaimable bytes and this machine's memory with a 40 px pressure bar.",
          "Machines are chips, not a count: hostname, memory share and the last 30 polls as a 2 px tracker strip. A chip turns amber when a session there waits on you and red when one is stuck; an unreachable machine is dashed; this machine is ringed in the accent. The bar sits one surface step dimmer than the content, with no rule under it.",
          "Below 1200 px the quiet cluster keeps only memory, one chip stays with a +n for the rest, and search keeps only its icon and key.",
          "Compare with *Clusters with fleet pulse*, which gives hygiene and the fleet their own clusters and folds the counts into one line when all is clear; and with *Compare › Today* for the header as it is now.",
        ].join("\n\n"),
      },
    },
  },
} satisfies Meta<typeof AttentionHeader>;
export default meta;
type Story = StoryObj<typeof meta>;

const needYou3 = /^Need you\s*3\s*up 2 in the last hour$/;

export const Busy: Story = {
  parameters: { docs: { description: { story: "Three agents wait on you (up 2 in the hour), ten turns await review (up 4), nothing is stuck (down 1). studio's chip is amber for its waiting session, build-01's red for its stuck one, build-02's dashed. Click a count to filter the inbox to its kind." } } },
  play: async ({ canvas, args }) => {
    const needYou = canvas.getByRole("button", { name: needYou3 });
    await expect(needYou).toBeVisible();
    await expect(needYou).toHaveAttribute("aria-pressed", "false");
    await expect(canvas.getByRole("button", { name: /^To review\s*10\s*up 4 in the last hour$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Stuck\s*0\s*down 1 in the last hour$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^studio\s*75% memory · this machine · 1 waiting · 5 working · 2 idle · seen 4s ago/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Memory on studio:\s*24.*32 GB.*75%/ })).toBeVisible();
    await userEvent.click(needYou);
    await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await userEvent.click(canvas.getByRole("button", { name: /^studio\s*75%/ }));
    await expect(args.onOpenFleet).toHaveBeenCalled();
    await userEvent.click(canvas.getByRole("button", { name: /^Memory on studio/ }));
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
  parameters: { docs: { description: { story: "Every count is zero and steady: the numbers go quiet and the chips show only green and blue polls." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Need you\s*0\s*steady over the last hour$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Stuck\s*0\s*steady over the last hour$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^studio\s*75% memory · this machine · 3 working · 1 idle/ })).toBeVisible();
  },
};

export const OneStuck: Story = {
  name: "One stuck",
  args: { ...oneStuck },
  parameters: { docs: { description: { story: "One session has gone quiet for too long: the Stuck count is the only one above zero, its delta is red, and build-01's chip turns red with it." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Stuck\s*1\s*up 1 in the last hour$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Need you\s*0\s*steady over the last hour$/ })).toBeVisible();
    // build-01's chip is in the DOM at every width; a laptop keeps it behind +n.
    await expect(canvas.getByText("build-01")).toBeInTheDocument();
  },
};

export const ManyMachines: Story = {
  name: "Many machines",
  args: { ...manyMachines },
  parameters: { docs: { description: { story: "Ten machines: three chips and a +7 that opens the Fleet (one chip and +9 on a laptop)." } } },
  play: async ({ canvas, args }) => {
    const more = canvas.getByRole("button", { name: /^\+(7|9)\s*more machines, open Fleet$/ });
    await expect(more).toBeVisible();
    await userEvent.click(more);
    await expect(args.onOpenFleet).toHaveBeenCalled();
  },
};

export const LaptopWidth: Story = {
  name: "Laptop width",
  globals: { viewport: { value: "laptop", isRotated: false } },
  parameters: { docs: { description: { story: "At 1024 px: the counts keep their size, the quiet cluster keeps only memory, one chip stays with +3 for the rest, and search keeps only its icon and key." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: needYou3 })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^studio\s*75%/ })).toBeVisible();
    await expect(canvas.queryByRole("button", { name: /^build-01/ })).toBeNull();
    await expect(canvas.getByRole("button", { name: /^\+3\s*more machines, open Fleet$/ })).toBeVisible();
    await expect(canvas.queryByRole("button", { name: /^Reclaimable/ })).toBeNull();
    await expect(canvas.getByRole("button", { name: /^Memory on studio/ })).toBeVisible();
  },
};

export const Filtering: Story = {
  args: { filter: "stuck" },
  parameters: { docs: { description: { story: "The inbox is filtered to stuck sessions: the count is pressed, with an inset ring in the accent." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Stuck\s*0\s*down 1 in the last hour$/ })).toHaveAttribute("aria-pressed", "true");
  },
};

export const KeyboardFocus: Story = {
  name: "Keyboard focus",
  parameters: { docs: { description: { story: "Tab reaches the first count, with the accent focus ring and its tooltip; Enter filters. Review on the canvas, where the Interactions panel replays it." } } },
  play: async ({ canvas, args }) => {
    await userEvent.tab();
    const needYou = canvas.getByRole("button", { name: needYou3 });
    await expect(needYou).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    await expect(args.onFilter).toHaveBeenCalledWith("waiting");
    await userEvent.tab();
    await expect(canvas.getByRole("button", { name: /^To review\s*10\s*up 4 in the last hour$/ })).toHaveFocus();
  },
};
