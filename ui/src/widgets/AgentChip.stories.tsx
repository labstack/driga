import { Group, Stack } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { AgentChip } from "./AgentChip";
import type { Agent } from "./types";

const families: Agent[] = ["claude", "codex", "gemini", "copilot", "agy"];

/** An agent family as a glyph in its categorical tint. */
const meta = {
  id: "widgets-agent-chip",
  title: "Components/Agent chip",
  component: AgentChip,
  args: { agent: "claude" },
  parameters: { docs: { description: { component: "A 20 px circle in the family's tint (claude peach, codex teal, gemini sapphire, copilot sky, agy pink) with its glyph, and optionally the name. None of the tints is a state color, so a chip is never read as a status; a small yellow dot says the agent waits on you. An adapter Driga does not know draws neutral." } } },
} satisfies Meta<typeof AgentChip>;
export default meta;
type Story = StoryObj<typeof meta>;

export const EveryFamily: Story = {
  render: () => <Stack gap="sm">
    <Group gap="md">{families.map(a => <AgentChip key={a} agent={a} />)}</Group>
    <Group gap="md">{families.map(a => <AgentChip key={a} agent={a} name />)}</Group>
    <Group gap="md">{families.map(a => <AgentChip key={a} agent={a} size={28} />)}</Group>
  </Stack>,
  play: async ({ canvas }) => {
    await expect(canvas.getAllByRole("img", { name: "Codex" })).toHaveLength(2);
    await expect(canvas.getByText("agy")).toBeVisible();
  },
};

export const Glyph: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Claude Code" })).toBeVisible(); } };
export const Named: Story = { args: { agent: "gemini", name: true }, play: async ({ canvas }) => { await expect(canvas.getByText("Gemini")).toBeVisible(); } };
export const Waiting: Story = { args: { agent: "codex", waiting: true }, play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Codex, waiting on you" })).toBeVisible(); } };
export const Unknown: Story = { args: { agent: "aider", name: true }, play: async ({ canvas }) => { await expect(canvas.getByText("aider")).toBeVisible(); } };
