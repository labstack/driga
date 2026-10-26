import { Box, SimpleGrid } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { KpiTile } from "./KpiTile";
import * as f from "./KpiTile-fixtures";

/** A stat with its context: delta, sparkline, quiet zero and attention tint. */
const meta: Meta<typeof KpiTile> = {
  id: "widgets-kpi-tile",
  title: "Drafts/Widgets/KPI tile",
  component: KpiTile,
  args: { onClick: fn() },
  // A tile alone is reviewed at its board width; the strip takes the board.
  decorators: [(Story, { parameters }) => <Box maw={parameters.maw ?? 232}><Story /></Box>],
  parameters: { docs: { description: { component: "A label, a large mono value with its unit, a delta chip that reads the metric's own polarity (down is good for memory, bad for sessions), and a sparkline. A zero is quiet: grey value and no chip. A count that needs the person tints the tile in its state color with a 2 px rule and a dot; only the waiting dot breathes. Clickable tiles are buttons and show their selection as an inset ring. Compare: Needs you (tinted), Quiet zero (grey), Memory (down is good, so the green caret points down)." } } },
};
export default meta;
type Story = StoryObj<typeof meta>;

export const NeedsYou: Story = {
  args: f.needYou,
  play: async ({ args, canvas, userEvent }) => {
    const tile = canvas.getByRole("button", { name: /Need you/ });
    await expect(tile).toHaveAttribute("data-attention", "warning");
    await expect(canvas.getByText("3")).toBeVisible();
    await expect(canvas.getByText("2 · 1h")).toBeVisible();
    await userEvent.click(tile);
    await expect(args.onClick).toHaveBeenCalled();
    await userEvent.unhover(tile);
  },
};

export const ToReview: Story = {
  args: f.toReview,
  play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /To review/ })).toHaveAttribute("data-attention", "info"); },
};

export const QuietZero: Story = {
  args: f.stuck,
  parameters: { docs: { description: { story: "Zero is calm: the value turns grey, the tint and the chip go, so a zero never looks like a ten." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /Stuck/ })).not.toHaveAttribute("data-attention");
    await expect(canvas.queryByText(/1h/)).toBeNull();
  },
};

export const StuckOne: Story = { args: f.stuckOne, play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /Stuck/ })).toHaveAttribute("data-attention", "danger"); } };

export const Memory: Story = {
  args: f.memory,
  parameters: { docs: { description: { story: "Memory falling is good, so a downward delta is green; the chip's tooltip says so in words." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("25")).toBeVisible();
    await expect(canvas.getByLabelText(/less than 1h ago, good/)).toBeVisible();
    await expect(canvas.getByRole("img", { name: /Memory · studio over the window/ })).toBeInTheDocument();
  },
};

export const Leftover: Story = { args: f.leftover, play: async ({ canvas }) => { await expect(canvas.getByText("steady · 1h")).toBeVisible(); } };

export const Selected: Story = {
  args: { ...f.needYou, selected: true },
  play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /Need you/ })).toHaveAttribute("aria-pressed", "true"); },
};

export const Static: Story = {
  args: { ...f.reclaimable, onClick: undefined },
  play: async ({ canvas }) => { await expect(canvas.queryByRole("button")).toBeNull(); await expect(canvas.getByText("Reclaimable")).toBeVisible(); },
};

export const Strip: Story = {
  args: f.needYou,
  render: args => <SimpleGrid cols={{ base: 2, md: 3, lg: 6 }} spacing="sm">{f.tiles.map(t => <KpiTile key={t.label} {...t} onClick={args.onClick} />)}</SimpleGrid>,
  parameters: { maw: 1400, docs: { description: { story: "The six Home metrics side by side: attention ranks over hygiene by tint alone." } } },
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("button")).toHaveLength(6); },
};
