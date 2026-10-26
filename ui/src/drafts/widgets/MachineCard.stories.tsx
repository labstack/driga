import { Box, SimpleGrid } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { MachineCard } from "./MachineCard";
import * as f from "./MachineCard-fixtures";

/** One machine as an instrument, with its 90-minute attention strip. */
const meta = {
  id: "widgets-machine-card",
  title: "Drafts/Widgets/Machine card",
  component: MachineCard,
  args: { machine: f.studio, onOpen: fn(), onReclaim: fn() },
  // A card alone is reviewed at its board width; a row of them takes the board.
  decorators: [(Story, { parameters }) => <Box maw={parameters.maw ?? 300}><Story /></Box>],
  parameters: { docs: { description: { component: "Hostname in mono with a local or mTLS tag and how Driga reaches it; memory as a bar gauge; sessions as a stacked state bar with the counts (zero counts hollow and grey); the last 90 minutes as a strip of the worst state per minute, with one tooltip that follows the pointer; and what is left over to reclaim. Compare Studio (local, calm), Build (94% memory, a stuck run in red on the strip), Laptop (unreachable, hollow dot) and Lost (not seen for over ten minutes)." } } },
} satisfies Meta<typeof MachineCard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Studio: Story = {
  play: async ({ args, canvas, userEvent, step }) => {
    await expect(canvas.getByText("studio")).toBeVisible();
    await expect(canvas.getByText("local")).toBeVisible();
    await expect(canvas.getByText("reachable")).toBeVisible();
    await expect(canvas.getByRole("img", { name: /studio, last 90 minutes/ })).toBeInTheDocument();
    await expect(canvas.getByText("2 leftover · 1.8 GB reclaimable")).toBeVisible();
    await step("Reclaim", async () => {
      await userEvent.click(canvas.getByRole("button", { name: "Reclaim" }));
      await expect(args.onReclaim).toHaveBeenCalledWith(f.studio);
    });
  },
};

export const Build: Story = {
  args: { machine: f.build },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("mTLS")).toBeVisible();
    await expect(canvas.getByText("94%")).toBeVisible();
    await expect(canvas.getByRole("img", { name: /12 stuck/ })).toBeInTheDocument();
  },
};

export const Laptop: Story = {
  args: { machine: f.laptop },
  play: async ({ canvas }) => { await expect(canvas.getByText("last seen 4m ago")).toBeVisible(); await expect(canvas.getByText("nothing left over")).toBeVisible(); },
};

export const Lost: Story = { args: { machine: f.laptopLost }, play: async ({ canvas }) => { await expect(canvas.getByText("last seen 38m ago")).toBeVisible(); } };

export const Fleet: Story = {
  render: args => <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="sm">{f.machines.map(m => <MachineCard key={m.name} {...args} machine={m} />)}</SimpleGrid>,
  parameters: { maw: 960, docs: { description: { story: "The three machines in a row, as the Fleet strip would show them." } } },
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("img", { name: /last 90 minutes/ })).toHaveLength(3); },
};
