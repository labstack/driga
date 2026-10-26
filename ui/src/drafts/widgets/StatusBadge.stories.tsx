import { Group, Stack, Text } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { StatusBadge } from "./StatusBadge";
import { stateOrder } from "./types";

/** A session state as dot and word, or as a tinted pill. */
const meta = {
  id: "widgets-status-badge",
  title: "Drafts/Widgets/Status badge",
  component: StatusBadge,
  args: { state: "working" },
  parameters: { docs: { description: { component: "Two forms. The dot and word is the default for tables and cards, and the only form green and yellow take as text in the light scheme. The pill (14% tint, a 1 px inset ring in the hue, the word) is for inbox kinds and filters. Idle's dot is hollow and finished is a check, so no two states differ by color alone. An inferred state, read from activity rather than reported, has a tilde and a dashed edge. The waiting dot may breathe once per widget." } } },
} satisfies Meta<typeof StatusBadge>;
export default meta;
type Story = StoryObj<typeof meta>;

export const EveryState: Story = {
  render: () => <Stack gap="sm">
    <Text size="xs" c="dimmed">Dot and word</Text>
    <Group gap="md">{stateOrder.map(s => <StatusBadge key={s} state={s} />)}</Group>
    <Text size="xs" c="dimmed">Pill</Text>
    <Group gap="xs">{stateOrder.map(s => <StatusBadge key={s} state={s} variant="pill" />)}</Group>
    <Text size="xs" c="dimmed">Inferred</Text>
    <Group gap="md">{stateOrder.map(s => <StatusBadge key={s} state={s} inferred />)}</Group>
  </Stack>,
  play: async ({ canvas }) => {
    await expect(canvas.getAllByLabelText("waiting")).toHaveLength(2);
    await expect(canvas.getByLabelText("stuck, inferred")).toBeVisible();
    await expect(canvas.getByText("~idle")).toBeVisible();
  },
};

export const Dot: Story = { play: async ({ canvas }) => { await expect(canvas.getByLabelText("working")).toBeVisible(); } };
export const Pill: Story = { args: { state: "waiting", variant: "pill", pulse: true }, play: async ({ canvas }) => { await expect(canvas.getByLabelText("waiting")).toBeVisible(); } };
export const Finished: Story = { args: { state: "finished" }, play: async ({ canvas }) => { await expect(canvas.getByText("finished")).toBeVisible(); } };
export const Inferred: Story = { args: { state: "idle", inferred: true }, play: async ({ canvas }) => { await expect(canvas.getByLabelText("idle, inferred")).toBeVisible(); } };
export const CustomWord: Story = { args: { state: "waiting", variant: "pill", label: "needs approval" }, play: async ({ canvas }) => { await expect(canvas.getByText("needs approval")).toBeVisible(); } };
