import { Box, Group, SimpleGrid } from "@mantine/core";
import { ChatCircle, SquaresFour } from "@phosphor-icons/react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, waitFor } from "storybook/test";
import { KpiTile } from "./KpiTile";
import * as f from "./KpiTile-fixtures";

/** A stat with its context: delta, sparkline, quiet zero and attention tint. */
const meta: Meta<typeof KpiTile> = {
  id: "widgets-kpi-tile",
  title: "Components/KPI tile",
  component: KpiTile,
  args: { onClick: fn() },
  // A tile alone is reviewed at its board width; the strip takes the board.
  decorators: [(Story, { parameters }) => <Box maw={parameters.maw ?? 232}><Story /></Box>],
  parameters: { docs: { description: { component: "A label, a large mono value with its unit, a delta chip that says the change in words (down is good for memory, bad for sessions), and a sparkline. A zero is quiet: grey value, no chip, no sparkline. A count that needs the person tints the tile in its state color with a 2 px rule and a dot; only the waiting dot breathes. Clickable tiles are buttons and show their selection as an inset ring. The header size is the same tile bare on the shell. Compare: Needs you (tinted), Quiet zero (grey), Memory (down is good, said in the chip's words)." } } },
};
export default meta;
type Story = StoryObj<typeof meta>;

export const NeedsYou: Story = {
  args: f.needYou,
  play: async ({ args, canvas, userEvent }) => {
    const tile = canvas.getByRole("button", { name: /Need you/ });
    await expect(tile).toHaveAttribute("data-attention", "warning");
    await expect(canvas.getByText("3")).toBeVisible();
    await expect(canvas.getByText("▲ 2 · 1h")).toBeVisible();
    await userEvent.click(tile);
    await expect(args.onClick).toHaveBeenCalled();
    await userEvent.unhover(tile);
    tile.blur();
  },
};

export const ToReview: Story = {
  args: f.toReview,
  play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /To review/ })).toHaveAttribute("data-attention", "info"); },
};

export const QuietZero: Story = {
  args: f.stuck,
  parameters: { docs: { description: { story: "Zero is calm: the value turns grey, the tint, the chip and the sparkline go, so a zero never looks like a ten." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /Stuck/ })).not.toHaveAttribute("data-attention");
    await expect(canvas.queryByText(/1h/)).toBeNull();
    await expect(canvas.queryByRole("img")).toBeNull();
  },
};

export const StuckOne: Story = { args: f.stuckOne, play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /Stuck/ })).toHaveAttribute("data-attention", "danger"); } };

export const Memory: Story = {
  args: f.memory,
  parameters: { docs: { description: { story: "Memory falling is good; the chip's arrow carries the direction and its words say it is good news. The tint stays neutral." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("25")).toBeVisible();
    await expect(canvas.getByText(/less than 1h ago, good/)).toBeInTheDocument();
    await expect(canvas.getByRole("img", { name: /Memory · studio over the last hour/ })).toBeInTheDocument();
  },
};

export const Leftover: Story = { args: f.leftover, play: async ({ canvas }) => { await expect(canvas.getByText("steady · 1h")).toBeVisible(); await expect(canvas.getByText("· 1.8 GB")).toBeVisible(); } };

export const Selected: Story = {
  args: { ...f.needYou, selected: true },
  play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: /Need you/ })).toHaveAttribute("aria-pressed", "true"); },
};

export const Static: Story = {
  args: { ...f.reclaimable, onClick: undefined },
  play: async ({ canvas }) => { await expect(canvas.queryByRole("button")).toBeNull(); await expect(canvas.getByText("Reclaimable")).toBeVisible(); },
};

export const WithMenu: Story = {
  name: "With a menu",
  args: { ...f.needYou, menu: [{ label: "Show on Home", icon: SquaresFour, onClick: fn() }, { label: "Ask Chat about it", icon: ChatCircle, onClick: fn() }] },
  parameters: { docs: { description: { story: "A tile's other actions wait behind a ⋯ in its corner, shown on hover or focus, and open on right-click too." } } },
  play: async ({ args, canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole("button", { name: "More for Need you" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Show on Home" }));
    await expect(args.menu![0].onClick).toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    await userEvent.unhover(canvas.getByRole("button", { name: /^Need you/ }));
  },
};

export const HeaderSize: Story = {
  name: "Header size",
  args: { ...f.needYou, size: "header", shortcut: "1" },
  decorators: [Story => <Group h={56} px="md" style={{ background: "var(--driga-ground)" }}><Story /></Group>],
  parameters: { maw: 400, docs: { description: { story: "In the 56 px header the tile is bare: the same tint and rule, the value at 20 px, no border and no chip." } } },
  play: async ({ canvas }) => {
    const tile = canvas.getByRole("button", { name: /^Need you\s*3/ });
    await expect(tile).toHaveAttribute("data-size", "header");
    await expect(tile).toHaveAttribute("aria-keyshortcuts", "1");
  },
};

export const Strip: Story = {
  args: f.needYou,
  render: args => <SimpleGrid cols={{ base: 2, md: 3, lg: 6 }} spacing="sm">{f.tiles.map(t => <KpiTile key={t.label} {...t} onClick={args.onClick} />)}</SimpleGrid>,
  parameters: { maw: 1400, docs: { description: { story: "Six metrics side by side, as a Chat-built board would lay them: attention ranks over hygiene by tint alone." } } },
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("button")).toHaveLength(6); },
};
