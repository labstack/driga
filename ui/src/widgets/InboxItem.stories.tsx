import { Box, Stack } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { InboxItem } from "./InboxItem";
import * as f from "./InboxItem-fixtures";

/** One thing that needs the person, in its kinds. */
const meta = {
  id: "widgets-inbox-item",
  title: "Components/Inbox item",
  component: InboxItem,
  args: { entry: f.permission, onApprove: fn(), onOpen: fn(), onDismiss: fn() },
  decorators: [Story => <Box maw={880}><Story /></Box>],
  parameters: { docs: { description: { component: "Three lines at most: a rule on the left in the kind's color (yellow needs you, red stuck, blue to review), the agent's chip with a yellow dot while it waits, the title with the kind in words, the reason in an inset box (the command a permission asks for, the question, a stuck session's last tool line, a machine's numbers), where it runs, how long ago and how long it has waited. Actions show on hover, focus or selection, with their keys (a, o, x) in tooltips and working from the keyboard on the row. Compare the kinds: Permission, Question, Finished with its review facts, Stuck, Machine warning." } } },
} satisfies Meta<typeof InboxItem>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Permission: Story = {
  play: async ({ args, canvas, userEvent, step }) => {
    await expect(canvas.getByText("needs approval")).toBeVisible();
    await expect(canvas.getByText("Bash(go test -race ./middleware/...)")).toBeVisible();
    await expect(canvas.getByText(/waiting 2m/)).toBeVisible();
    await step("Approve from the hover actions", async () => {
      await userEvent.hover(canvas.getByText("Fix the race in the rate limiter"));
      await userEvent.click(canvas.getByRole("button", { name: /^Approve/ }));
      await expect(args.onApprove).toHaveBeenCalledWith(f.permission);
      await userEvent.unhover(canvas.getByText("Fix the race in the rate limiter"));
    });
  },
};

export const PermissionWithDeny: Story = {
  name: "Permission, allow or deny",
  args: { onDeny: fn(), armed: false },
  parameters: { docs: { description: { story: "A Chat permission prompt can be denied too. Both answers arm after a moment, so a click meant for the row that was there before never answers the one that moved into place." } } },
  play: async ({ canvas, userEvent }) => {
    await userEvent.hover(canvas.getByText("Fix the race in the rate limiter"));
    await expect(canvas.getByRole("button", { name: /^Deny/ })).toBeDisabled();
    await expect(canvas.getByRole("button", { name: /^Approve/ })).toBeDisabled();
    await userEvent.unhover(canvas.getByText("Fix the race in the rate limiter"));
  },
};

export const Question: Story = {
  args: { entry: f.question },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("asks a question")).toBeVisible();
    await expect(canvas.getByText(/Should retries back off/)).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Answer/ })).toBeInTheDocument();
  },
};

export const Finished: Story = {
  args: { entry: f.finished },
  parameters: { docs: { description: { story: "A finished turn carries its review facts: files edited, uncommitted (yellow, it needs a decision) and unpushed." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("finished · 16 turns")).toBeVisible();
    await expect(canvas.getByText("4 files")).toBeVisible();
    await expect(canvas.getByText("1 uncommitted")).toBeVisible();
    await expect(canvas.getByText("2 unpushed")).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Mark reviewed/ })).toBeInTheDocument();
  },
};

export const FinishedClean: Story = { args: { entry: f.finishedClean }, play: async ({ canvas }) => { await expect(canvas.getByText("2 files")).toBeVisible(); await expect(canvas.queryByText(/uncommitted/)).toBeNull(); } };

export const Stuck: Story = {
  args: { entry: f.stuck },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("quiet 14m, expected 3m")).toBeVisible();
    await expect(canvas.getByText("quiet 14m 0s")).toBeVisible();
    await expect(canvas.queryByRole("button", { name: /^Approve/ })).toBeNull();
  },
};

export const MachineWarning: Story = {
  args: { entry: f.machine, onApprove: undefined },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("img", { name: "machine build-01" })).toBeInTheDocument();
    await expect(canvas.getByText("build-01 is at 94% memory")).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Dismiss/ })).toBeInTheDocument();
  },
};

export const Selected: Story = {
  args: { selected: true },
  parameters: { docs: { description: { story: "A selected row keeps its actions visible and shows the inset ring." } } },
  play: async ({ args, canvas, userEvent, step }) => {
    await expect(canvas.getByRole("button", { name: /^Open/ })).toBeVisible();
    await step("Keys on the focused row: o opens, x dismisses", async () => {
      (canvas.getByText("Fix the race in the rate limiter").closest(".w-row") as HTMLElement).focus();
      await userEvent.keyboard("o");
      await expect(args.onOpen).toHaveBeenCalledWith(f.permission);
      await userEvent.keyboard("x");
      await expect(args.onDismiss).toHaveBeenCalledWith(f.permission);
    });
  },
};

export const LongTitle: Story = {
  args: { entry: f.longTitle },
  decorators: [Story => <Box maw={640}><Story /></Box>],
  parameters: { docs: { description: { story: "A long title and command truncate; the full text is on hover." } } },
  play: async ({ canvas }) => { await expect(canvas.getByTitle(f.longTitle.title)).toBeVisible(); await expect(canvas.getByTitle(f.longTitle.reason!)).toBeVisible(); },
};

export const Queue: Story = {
  render: args => <Stack gap={4}>{f.entries.map(e => <InboxItem key={e.id} {...args} entry={e} />)}</Stack>,
  parameters: { docs: { description: { story: "The kinds stacked as the Inbox panel shows them." } } },
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("button", { name: /^Open/ })).toHaveLength(6); },
};
