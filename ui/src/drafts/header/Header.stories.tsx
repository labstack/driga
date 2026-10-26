import { ActionIcon, Group, Tooltip } from "@mantine/core";
import { ChatCircle } from "@phosphor-icons/react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { fragments } from "../../fixtures/fragments";
import { Brand } from "../../host/Brand";
import { Metrics } from "../../host/Metrics";
import { Search } from "../../host/Search";
import { AttentionHeader } from "./AttentionHeader";
import { ClustersHeader } from "./ClustersHeader";
import { busy } from "./Header-fixtures";

/** The header as it is now: the app's own parts in the app's own row. */
function Today() {
  return <Group component="header" h={56} px="md" gap="md" wrap="nowrap" style={{ background: "color-mix(in srgb, var(--driga-ground) 88%, transparent)", borderBottom: "1px solid var(--driga-border)" }}>
    <Brand />
    <Metrics fragment={fragments.header} editing={false} onAsk={fn()} onShow={fn()} onRemove={fn()} />
    <Search list={[]} act={fn(async () => undefined)} ask={fn()} open={fn()} say={fn()} />
    <Tooltip label="Hide Chat"><ActionIcon size="lg" variant="light" color="brand" aria-pressed aria-label="Chat"><ChatCircle size={20} /></ActionIcon></Tooltip>
  </Group>;
}

/** The two header drafts and today's header, one under the other, over
 * the same busy fleet, so the owner can compare them on one page. */
const meta = {
  id: "header",
  title: "Drafts/Header/Compare",
  component: ClustersHeader,
  args: { ...busy, filter: null, chatOpen: true, onFilter: fn(), onOpenFleet: fn(), onSearch: fn(), onToggleChat: fn(), onShow: fn() },
  parameters: {
    layout: "fullscreen",
    docs: {
      story: { inline: false, iframeHeight: 120 },
      description: {
        component: [
          "The two header drafts and today's header over the same busy fleet: three agents waiting on you, ten turns to review, nothing stuck, two leftover process trees, memory at 75% and falling, four machines with one unreachable.",
          "*Clusters with fleet pulse* keeps every number the header shows today, ranked: attention tinted, hygiene quiet, this machine with its trend, the fleet as dots. It folds to one line when nothing needs you. *Attention bar with machine chips* keeps only the three counts large and pushes the rest to a quiet right-hand cluster, naming machines with a tracker strip each; it stays the same shape when all is clear. Both take the same props, so the choice is presentation only.",
          "Each draft has its own page with every state: busy, all clear, one stuck, many machines, laptop width, filtering and keyboard focus.",
        ].join("\n\n"),
      },
    },
  },
} satisfies Meta<typeof ClustersHeader>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ClustersWithFleetPulse: Story = {
  name: "Clusters with fleet pulse",
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Need you\s*3$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Fleet: 4 machines/ })).toBeVisible();
  },
};

export const AttentionBarWithMachineChips: Story = {
  name: "Attention bar with machine chips",
  render: args => <AttentionHeader {...args} />,
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("button", { name: /^Need you\s*3\s*up 2 in the last hour$/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^studio\s*75% memory · this machine/ })).toBeVisible();
  },
};

export const TodaysHeader: Story = {
  name: "Today",
  render: () => <Today />,
  parameters: { docs: { description: { story: "The header as the app renders it now: Brand, the Metrics row from the header fragment, Search and the Chat toggle." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("img", { name: "Driga" })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Need you\s*3/ })).toBeVisible();
    await expect(canvas.getByRole("button", { name: /^Possibly stuck\s*1/ })).toBeVisible();
  },
};
