import { Box, Stack } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { EmptyState } from "./EmptyState";

const checkedAt = "2026-03-14T14:59:40Z";

/** A panel with nothing to show, kept to one calm row. */
const meta = {
  id: "widgets-empty-state",
  title: "Drafts/Widgets/Empty state",
  component: EmptyState,
  args: { kind: "all-clear", title: "Nothing needs you", caption: "7 sessions working", checkedAt },
  decorators: [Story => <Box maw={560}><Story /></Box>],
  parameters: { docs: { description: { component: "One 56 px row, never a void: a tinted circle with an icon, the message, and a caption with the count and time that explain the emptiness. All-clear is green with the check time; a plain empty says what will appear and how, with at most one action; an error is red with a retry and the command to run by hand in the inset mono box." } } },
} satisfies Meta<typeof EmptyState>;
export default meta;
type Story = StoryObj<typeof meta>;

export const AllClear: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("status")).toHaveTextContent("Nothing needs you");
    await expect(canvas.getByText(/checked \d/)).toBeVisible();
  },
};

export const Empty: Story = {
  args: { kind: "empty", title: "No sessions on laptop-02 yet", caption: "Start one in a terminal; Driga notices within a few seconds.", checkedAt: undefined, action: { label: "Learn how", onClick: fn() } },
  play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: "Learn how" })).toBeVisible(); },
};

export const Failed: Story = {
  args: { kind: "error", title: "The service did not answer", caption: "Driga will try again on its own.", checkedAt: undefined, command: "driga service logs", onRetry: fn() },
  play: async ({ args, canvas, userEvent }) => {
    await expect(canvas.getByRole("alert")).toHaveTextContent("driga service logs");
    await userEvent.click(canvas.getByRole("button", { name: "Retry" }));
    await expect(args.onRetry).toHaveBeenCalled();
  },
};

export const Stacked: Story = {
  render: () => <Stack gap={0}>
    <EmptyState kind="all-clear" title="Nothing needs you" caption="7 sessions working" checkedAt={checkedAt} />
    <EmptyState kind="empty" title="No sessions on laptop-02 yet" caption="Start one in a terminal; Driga notices within a few seconds." action={{ label: "Learn how", onClick: fn() }} />
    <EmptyState kind="error" title="The service did not answer" command="driga service logs" onRetry={fn()} />
  </Stack>,
  play: async ({ canvas }) => { await expect(canvas.getAllByRole("status")).toHaveLength(2); await expect(canvas.getByRole("alert")).toBeVisible(); },
};
