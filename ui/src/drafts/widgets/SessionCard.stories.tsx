import { Box, Stack } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { SessionCard } from "./SessionCard";
import * as f from "./SessionCard-fixtures";

/** One session as a two-column card, and its compact row. */
const meta = {
  id: "widgets-session-card",
  title: "Drafts/Widgets/Session card",
  component: SessionCard,
  args: { session: f.working, onOpen: fn(), onApprove: fn() },
  decorators: [Story => <Box maw={520}><Story /></Box>],
  parameters: { docs: { description: { component: "Left: state and title, agent chip with project, branch and machine, the last tool line in the inset box, and counters (turns, files, uncommitted in yellow, memory). Right: a sparkline of tool calls per minute in the state's color and the one action that fits (Approve while waiting, otherwise Open terminal). The footer says when it started and how long it has been quiet, in yellow once longer than usual. The compact form is one row for a Fleet inspector. Compare Working, Waiting, Stuck, Idle (inferred) and Finished; then Compact." } } },
} satisfies Meta<typeof SessionCard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Working: Story = {
  play: async ({ args, canvas, userEvent }) => {
    await expect(canvas.getByText("Fix the race in the rate limiter")).toBeVisible();
    await expect(canvas.getByText(/Edit\(internal\/middleware\/limit\.go\)/)).toBeVisible();
    await expect(canvas.getByText("16 turns")).toBeVisible();
    await expect(canvas.getByText("1 uncommitted")).toBeVisible();
    await expect(canvas.getByText("started 2h 2m ago")).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Open terminal" }));
    await expect(args.onOpen).toHaveBeenCalledWith(f.working);
  },
};

export const Waiting: Story = {
  args: { session: f.waiting },
  play: async ({ args, canvas, userEvent }) => {
    await expect(canvas.getByLabelText("waiting")).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Approve" }));
    await expect(args.onApprove).toHaveBeenCalledWith(f.waiting);
  },
};

export const Stuck: Story = {
  args: { session: f.stuck },
  parameters: { docs: { description: { story: "Quiet 14 minutes against the usual 3: the footer's quiet turns yellow." } } },
  play: async ({ canvas }) => { await expect(canvas.getByTitle("Quiet longer than the usual 3m")).toHaveTextContent("quiet 14m"); },
};

export const Idle: Story = { args: { session: f.idle }, play: async ({ canvas }) => { await expect(canvas.getByLabelText("idle, inferred")).toBeVisible(); } };

export const Finished: Story = { args: { session: f.finished }, play: async ({ canvas }) => { await expect(canvas.getByText("finished")).toBeVisible(); } };

export const Selected: Story = { args: { selected: true }, play: async ({ canvas }) => { await expect(canvas.getByText("Fix the race in the rate limiter").closest(".w-row")).toHaveAttribute("data-selected", "true"); } };

export const Compact: Story = {
  args: { compact: true },
  play: async ({ canvas }) => { await expect(canvas.getByText("384 MB")).toBeVisible(); await expect(canvas.queryByText(/started/)).toBeNull(); },
};

export const CompactList: Story = {
  render: args => <Stack gap={6}>{f.sessions.map(s => <SessionCard key={s.id} {...args} session={s} compact />)}</Stack>,
  parameters: { docs: { description: { story: "Every state as a compact row, in the order waiting, stuck, working, finished, idle." } } },
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("button")).toHaveLength(5); },
};
