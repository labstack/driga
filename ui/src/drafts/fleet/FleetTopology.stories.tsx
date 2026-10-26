import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { busy, empty, oneMachine, sessions, unreachable } from "./Fleet-fixtures";
import { FleetTopology } from "./FleetTopology";

const pagerSession = sessions.find(s => s.id === "099")!;

/** mapReady waits until the canvas has loaded, laid out and fitted the fleet. */
async function mapReady(canvas: ReturnType<typeof within>) {
  const map = await canvas.findByRole("application", { name: "Fleet map" });
  await waitFor(() => expect(map).toHaveAttribute("aria-busy", "false"));
  return map;
}

/** Variant B of the Fleet view: a machine strip, then the fleet as nested
 * boxes on a React Flow canvas laid out by ELK, with the inspector beside it. */
const meta = {
  id: "fleet-topology",
  title: "Drafts/Fleet/B · Topology",
  component: FleetTopology,
  args: { ...busy, onSelect: fn(), onOpen: fn(), onReply: fn() },
  decorators: [Story => <Box h={900} p="md" style={{ display: "flex", flexDirection: "column" }}><Story /></Box>],
  globals: { viewport: { value: "desktop", isRotated: false } },
  parameters: {
    layout: "fullscreen",
    docs: {
      story: { inline: false, iframeHeight: 900 },
      description: { component: "Machine boxes hold project boxes hold session cards, each card a real component: a status ring whose colour is the state and whose arcs are the last 30 turns, the agent, a 30-turn tracker strip, memory, the last tool, and Reply or Open. A grey edge joins a parent to the subagent it spawned; a red dashed edge joins two sessions editing the same file, here across machines. Hovering a card or box isolates its neighbourhood; clicking or pressing Enter on a focused one fills the inspector. ELK's layered layout keeps positions fixed for the same fleet; nothing drags and nothing simulates. The canvas pans and zooms, and a minimap can be switched on. React Flow and ELK load only when this view opens. Compare with A · Sankey, which draws the same fleet as a flow whose width is memory." },
    },
  },
} satisfies Meta<typeof FleetTopology>;
export default meta;
type Story = StoryObj<typeof meta>;

export const BusyFleet: Story = {
  parameters: { docs: { description: { story: "Three machines in a row, atlas boxed on two of them, fourteen cards, two spawn edges and the red clock.go overlap between studio and build-01." } } },
  play: async ({ canvas, args }) => {
    const map = await mapReady(canvas);
    const inMap = within(map);
    await expect(inMap.getByRole("group", { name: /^studio, local, reachable/ })).toBeVisible();
    await expect(inMap.getByRole("group", { name: /^laptop-02, last seen 14m ago/ })).toBeVisible();
    await expect(inMap.getAllByRole("group", { name: /, (working|waiting|possibly stalled|idle|finished), / })).toHaveLength(14);
    await expect(inMap.getByRole("group", { name: "Write the release notes spawned Review the changelog wording" })).toBeInTheDocument();
    await expect(inMap.getByRole("group", { name: /both edit clock\.go/ })).toBeInTheDocument();
    const flaky = inMap.getByRole("group", { name: /^Fix the flaky lifeline test, working/ });
    const load = inMap.getByRole("group", { name: /^Beacon load test, possibly stalled/ });
    await userEvent.hover(flaky);
    await waitFor(() => expect(load.querySelector("[data-dimmed]")).toHaveAttribute("data-dimmed", "true"));
    await expect(flaky.querySelector("[data-dimmed]")).toHaveAttribute("data-dimmed", "false");
    await userEvent.unhover(flaky);
    await waitFor(() => expect(load.querySelector("[data-dimmed]")).toHaveAttribute("data-dimmed", "false"));
    await userEvent.click(flaky);
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "session", id: "112" });
    await expect(within(canvas.getByRole("group", { name: "Inspector" })).getByRole("heading", { name: "Fix the flaky lifeline test" })).toBeVisible();
    await userEvent.unhover(flaky);
    await waitFor(() => expect(load.querySelector("[data-dimmed]")).toHaveAttribute("data-dimmed", "false"));
  },
};

export const OneMachine: Story = {
  args: oneMachine,
  parameters: { docs: { description: { story: "The local machine alone: one box, three projects." } } },
  play: async ({ canvas }) => {
    const inMap = within(await mapReady(canvas));
    await expect(inMap.getAllByRole("group", { name: /^(studio|build-01|laptop-02),/ })).toHaveLength(1);
    await expect(inMap.getAllByRole("group", { name: /, (working|waiting|possibly stalled|idle|finished), / })).toHaveLength(6);
  },
};

export const UnreachableMachine: Story = {
  args: unreachable,
  parameters: { docs: { description: { story: "An unreachable machine is a dashed box with a hollow dot and when it was last seen; its cards show what was last known." } } },
  play: async ({ canvas, args }) => {
    const inMap = within(await mapReady(canvas));
    const box = inMap.getByRole("group", { name: /^laptop-02, last seen 14m ago, 3 sessions/ });
    await expect(box).toBeVisible();
    await expect(box.querySelector("[data-reachable]")).toHaveAttribute("data-reachable", "false");
    await userEvent.click(box);
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "machine", id: "laptop-02" });
    await expect(within(canvas.getByRole("group", { name: "Inspector" })).getByText("unreachable · last seen 14m ago")).toBeVisible();
    await userEvent.unhover(box);
  },
};

export const SelectedSession: Story = {
  args: { defaultSelected: { kind: "session", id: pagerSession.id } },
  parameters: { docs: { description: { story: "A waiting subagent selected: its card takes the accent, and the inspector shows the question it asks, who spawned it, and Reply beside Open. The card's own Reply works too." } } },
  play: async ({ canvas, args }) => {
    const inMap = within(await mapReady(canvas));
    const card = inMap.getByRole("group", { name: /^Check the pager API/ });
    await expect(card.querySelector("[data-selected]")).toHaveAttribute("data-selected", "true");
    const inspector = within(canvas.getByRole("group", { name: "Inspector" }));
    await expect(inspector.getByRole("heading", { name: pagerSession.title })).toBeVisible();
    await expect(inspector.getByText(pagerSession.waiting!)).toBeVisible();
    await expect(inspector.getByText("Spawned by")).toBeVisible();
    await userEvent.click(within(card).getByRole("button", { name: "Reply" }));
    await expect(args.onReply).toHaveBeenCalledWith(pagerSession);
    await expect(args.onSelect).not.toHaveBeenCalled();
    await userEvent.unhover(card);
    await waitFor(() => expect(inMap.getByRole("group", { name: /^Beacon load test, possibly stalled/ }).querySelector("[data-dimmed]")).toHaveAttribute("data-dimmed", "false"));
  },
};

export const WithMinimap: Story = {
  parameters: { docs: { description: { story: "The minimap is off by default; the header's switch turns it on." } } },
  play: async ({ canvas }) => {
    await mapReady(canvas);
    await expect(canvas.queryByRole("img", { name: "Minimap of the fleet" })).toBeNull();
    await userEvent.click(canvas.getByRole("switch", { name: "Minimap" }));
    await expect(await canvas.findByRole("img", { name: "Minimap of the fleet" })).toBeVisible();
  },
};

export const EmptyFleet: Story = {
  args: empty,
  parameters: { docs: { description: { story: "No machines yet: the canvas is not even loaded." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("No machines yet")).toBeVisible();
    await expect(canvas.queryByRole("application")).toBeNull();
  },
};
