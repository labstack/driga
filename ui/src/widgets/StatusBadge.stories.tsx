import { Group, Stack, Text } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { StatusBadge, stateOf } from "./StatusBadge";
import { stateOrder } from "./types";

/** A session state as dot and word, or as a tinted pill. */
const meta = {
  id: "widgets-status-badge",
  title: "Components/Status badge",
  component: StatusBadge,
  args: { state: "working" },
  parameters: { docs: { description: { component: "Two forms. The dot and word is the default for tables, cards, legends and the Fleet tree. The pill (the state's tint, a 1 px inset ring in the hue, the word) is for inbox group headers and filters, and the state at the top of a session's detail. Green working, yellow waiting, red stuck, blue idle; finished is neutral with a check and idle's dot is hollow, so no two states differ by color alone. A state the service inferred from activity rather than read from the agent has a tilde and a dashed edge; its \"stalled?\" is the inferred form of stuck. A label that is no state, such as dead, draws neutral." } } },
} satisfies Meta<typeof StatusBadge>;
export default meta;
type Story = StoryObj<typeof meta>;

const labels = ["waiting", "working", "~working", "idle", "finished", "stalled?", "dead", "unknown"];

export const EveryState: Story = {
  render: () => <Stack gap="sm">
    <Text size="xs" c="dimmed">Dot and word</Text>
    <Group gap="md">{stateOrder.map(s => <StatusBadge key={s} state={s} />)}</Group>
    <Text size="xs" c="dimmed">Pill</Text>
    <Group gap="xs">{stateOrder.map(s => <StatusBadge key={s} state={s} variant="pill" />)}</Group>
    <Text size="xs" c="dimmed">Inferred</Text>
    <Group gap="md">{stateOrder.map(s => <StatusBadge key={s} state={s} inferred />)}</Group>
    <Text size="xs" c="dimmed">As the service labels them</Text>
    <Group gap="md">{labels.map(l => <StatusBadge key={l} {...stateOf(l)} />)}</Group>
  </Stack>,
  play: async ({ canvas }) => {
    await expect(canvas.getAllByLabelText("waiting")).toHaveLength(3);
    await expect(canvas.getAllByLabelText("stuck, inferred")).toHaveLength(2);
    await expect(canvas.getAllByText("~idle")).toHaveLength(1);
    await expect(canvas.getByLabelText("dead")).toBeVisible();
  },
};

export const Dot: Story = { play: async ({ canvas }) => { await expect(canvas.getByLabelText("working")).toBeVisible(); } };
export const Pill: Story = { args: { state: "waiting", variant: "pill", pulse: true }, play: async ({ canvas }) => { await expect(canvas.getByLabelText("waiting")).toBeVisible(); } };
export const Finished: Story = { args: { state: "finished" }, play: async ({ canvas }) => { await expect(canvas.getByText("finished")).toBeVisible(); } };
export const Inferred: Story = { args: { state: "idle", inferred: true }, play: async ({ canvas }) => { await expect(canvas.getByLabelText("idle, inferred")).toBeVisible(); } };
export const Stalled: Story = { name: "Stalled, as the service says it", args: stateOf("~stalled?"), play: async ({ canvas }) => { await expect(canvas.getByLabelText("stuck, inferred")).toBeVisible(); } };
export const CustomWord: Story = { args: { state: "waiting", variant: "pill", label: "Needs you · 2" }, play: async ({ canvas }) => { await expect(canvas.getByText("Needs you · 2")).toBeVisible(); } };
