import { Group, Stack, Text } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { StatusBadge, toneOf } from "./StatusBadge";

const states = ["waiting", "working", "idle", "finished", "stalled?", "dead", "unknown"];

/** A session's state, as the Sessions table, search and the timeline show it. */
const meta = {
  id: "components-status-badge",
  title: "Components/Status badge",
  component: StatusBadge,
  args: { tone: "info", children: "working" },
  parameters: { docs: { description: { component: "Waiting on a person is the one warning and a suspected stall the one danger; working is information, kept apart from success. An inferred state, read from process and file activity rather than the agent, has a dashed edge." } } },
} satisfies Meta<typeof StatusBadge>;
export default meta;
type Story = StoryObj<typeof meta>;

export const EveryState: Story = {
  render: () => <Stack gap="xs">
    <Group gap="xs">{states.map(s => <StatusBadge key={s} tone={toneOf(s)}>{s}</StatusBadge>)}</Group>
    <Text size="xs" c="dimmed">Inferred</Text>
    <Group gap="xs">{states.map(s => <StatusBadge key={s} tone={toneOf(s)} inferred>{s}</StatusBadge>)}</Group>
  </Stack>,
  play: async ({ canvas }) => {
    await expect(canvas.getAllByText("waiting")).toHaveLength(2);
    await expect(canvas.getByLabelText("stalled?, inferred")).toBeVisible();
  },
};

export const Working: Story = {};
export const Inferred: Story = { args: { tone: "info", children: "working", inferred: true } };
export const Waiting: Story = { args: { tone: "warning", children: "waiting" } };
export const Stalled: Story = { args: { tone: "danger", children: "stalled?", inferred: true } };
