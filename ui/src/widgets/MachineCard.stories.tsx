import { Box, SimpleGrid } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { MachineCard } from "./MachineCard";
import * as f from "./MachineCard-fixtures";

/** One machine as an instrument, with its 90-minute attention strip. */
const meta = {
  id: "widgets-machine-card",
  title: "Components/Machine card",
  component: MachineCard,
  args: { machine: f.studio, onOpen: fn(), onReclaim: fn() },
  // A card alone is reviewed at its board width; a row of them takes the board.
  decorators: [(Story, { parameters }) => <Box maw={parameters.maw ?? 300}><Story /></Box>],
  parameters: { docs: { description: { component: "Hostname in mono with a local or mTLS tag and how Driga reaches it; memory as a bar gauge; sessions as dot counts by state (zero counts hollow and grey); the last 90 minutes as a strip of the worst state per minute, with one tooltip that follows the pointer, when Driga has the history; and what is left over to reclaim. One card for Home and the Fleet strip, where it is a toggle that narrows the view to the machine. Compare Studio (local, calm), Build (94% memory, a stuck run in red on the strip), Laptop (unreachable: hollow dot, hollow cells), Lost (not seen for over ten minutes) and Fresh (no history yet)." } } },
} satisfies Meta<typeof MachineCard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Cpu: Story = {
  args: { machine: { ...f.studio, cpuPercent: 19 } },
  play: async ({ canvas }) => {
    await expect(canvas.getAllByText("19%")).toHaveLength(1);
    await expect(canvas.queryByText("/ 100%")).toBeNull();
    await expect(canvas.getByRole("progressbar", { name: "CPU on studio: 19% of 100%, 19%" })).toBeVisible();
  },
};

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
  play: async ({ canvas }) => {
    await expect(canvas.getByText("last seen 4m ago")).toBeVisible();
    await expect(canvas.getByText("nothing left over")).toBeVisible();
    await expect(canvas.getByRole("img", { name: /4 unseen/ })).toBeInTheDocument();
  },
};

export const Lost: Story = { args: { machine: f.laptopLost }, play: async ({ canvas }) => { await expect(canvas.getByText("last seen 38m ago")).toBeVisible(); } };

export const Fresh: Story = {
  args: { machine: f.fresh },
  parameters: { docs: { description: { story: "A machine Driga has only just met: no strip until it has watched a while." } } },
  play: async ({ canvas }) => { await expect(canvas.queryByRole("img")).toBeNull(); await expect(canvas.getByText("lab-01")).toBeVisible(); },
};

export const Selectable: Story = {
  args: { onSelect: fn(), onOpen: undefined, onReclaim: undefined, selected: false },
  parameters: { docs: { description: { story: "In the Fleet strip the card is a toggle: pressed, it narrows the view to the machine." } } },
  play: async ({ args, canvas, userEvent }) => {
    const card = canvas.getByRole("button", { name: /^studio local reachable/ });
    await expect(card).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(card);
    await expect(args.onSelect).toHaveBeenCalledWith(f.studio);
    await userEvent.unhover(card);
    card.blur();
  },
};

export const Selected: Story = {
  args: { onSelect: fn(), onOpen: undefined, onReclaim: undefined, selected: true },
  play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /^studio/ })).toHaveAttribute("aria-pressed", "true"); },
};

export const Fleet: Story = {
  render: args => <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="sm">{f.machines.map(m => <MachineCard key={m.name} {...args} machine={m} />)}</SimpleGrid>,
  parameters: { maw: 960, docs: { description: { story: "The three machines in a row, as Home would show them." } } },
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("img", { name: /last 90 minutes/ })).toHaveLength(3); },
};

export const Pressure: Story = {
 args: { machine: { ...f.studio, cpuPercent: 42, diskFree: 100 * 1024 ** 3, diskTotal: 500 * 1024 ** 3 } },
 play: async ({ canvas }) => {
  await expect(canvas.getByRole("progressbar", { name: /CPU on studio: 42%/ })).toBeVisible();
  await expect(canvas.getByRole("img", { name: /^Memory:.*69%/ })).toBeVisible();
  await expect(canvas.getByRole("img", { name: /^Disk:.*80%/ })).toBeVisible();
 },
};
