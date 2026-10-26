import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { minutes } from "../widgets/TrackerBar-fixtures";
import { bare, busy, empty, oneMachine, unreachable, waitingSession } from "./Fleet-fixtures";
import { FleetSankey } from "./FleetSankey";

const strips = { studio: { cells: minutes(31, { waits: 2 }).slice(-30), from: "30 polls", to: "now", label: "studio, last 30 polls" }, "build-01": { cells: minutes(32, { stuck: true }).slice(-30), from: "30 polls", to: "now", label: "build-01, last 30 polls" } };

/** The Fleet view: a machine strip, then the fleet as a sankey from
 * machines through projects to sessions, with the inspector beside it. */
const meta = {
  id: "fleet-sankey",
  title: "Components/Fleet/Fleet view",
  component: FleetSankey,
  args: { ...busy, strips, onSelect: fn(), onOpen: fn(), onReply: fn() },
  decorators: [Story => <Box h={900} p="md" style={{ display: "flex", flexDirection: "column" }}><Story /></Box>],
  globals: { viewport: { value: "desktop", isRotated: false } },
  parameters: {
    layout: "fullscreen",
    docs: {
      story: { inline: false, iframeHeight: 900 },
      description: { component: "Link width is memory, with a minimum to keep small sessions visible, and node colour is state, so the picture answers \"where is my RAM going\" and \"what needs me\" at once. Node order never changes between refreshes: machines local first then by name, projects by name, sessions by project then state. Hovering a node lights its whole path; a waiting session's path is yellow from the machine column on; leftovers hang off each machine as a dashed pseudo-project. Spawn and file-overlap relations are glyphs on the session label (↳ and a red !) and rows in the inspector, since they would break the flow. Tree mode is the same fleet as an accessible table and the mode for the keyboard. Below 1200 px the inspector moves under the chart." },
    },
  },
} satisfies Meta<typeof FleetSankey>;
export default meta;
type Story = StoryObj<typeof meta>;

export const BusyFleet: Story = {
  parameters: { docs: { description: { story: "Three machines, five projects (atlas on two of them), fourteen sessions in every state, leftovers on two machines. Nothing is selected, so the inspector waits." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("heading", { name: "Fleet" })).toBeVisible();
    await expect(canvas.getByText("3 machines · 5 projects · 14 sessions")).toBeVisible();
    await expect(canvas.getByText("· 5 need you")).toBeVisible();
    await expect(canvas.getAllByRole("button", { name: /reachable|last seen/ })).toHaveLength(3);
    await expect(canvas.getByRole("img", { name: /Fleet flow/ })).toBeVisible();
    await expect(canvas.getByText("width = memory (minimum shown) · colour = state")).toBeVisible();
    await expect(within(canvas.getByRole("group", { name: "Inspector" })).getByText("Nothing selected")).toBeVisible();
  },
};

export const OneMachine: Story = {
  args: oneMachine,
  parameters: { docs: { description: { story: "A fresh install: the local machine alone, with its three projects." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("1 machine · 3 projects · 6 sessions")).toBeVisible();
    await expect(canvas.getAllByRole("button", { name: /reachable/ })).toHaveLength(1);
    await expect(canvas.getByRole("img", { name: /Fleet flow/ })).toBeVisible();
  },
};

export const UnreachableMachine: Story = {
  args: unreachable,
  parameters: { docs: { description: { story: "A machine Driga cannot reach keeps its place with a hollow dot and when it was last seen; its sessions show as last known, with dashed badges." } } },
  play: async ({ canvas, args }) => {
    const card = canvas.getByRole("button", { name: /^laptop-02 last seen 14m ago/ });
    await expect(card).toBeVisible();
    await userEvent.click(card);
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "machine", id: "laptop-02" });
    const inspector = within(canvas.getByRole("group", { name: "Inspector" }));
    await expect(inspector.getByText("unreachable · last seen 14m ago")).toBeVisible();
    await expect(inspector.getByText("69% used, last known")).toBeVisible();
    await expect(inspector.getByText("1 waiting")).toBeVisible();
    await userEvent.unhover(card);
    card.blur();
  },
};

export const SelectedSession: Story = {
  args: { defaultSelected: { kind: "session", id: waitingSession.id, machine: waitingSession.machine } },
  parameters: { docs: { description: { story: "A waiting session selected: the inspector shows why it waits, its files, its subagent, and Reply beside Open." } } },
  play: async ({ canvas, args }) => {
    const inspector = within(canvas.getByRole("group", { name: "Inspector" }));
    await expect(inspector.getByRole("heading", { name: waitingSession.title })).toBeVisible();
    await expect(inspector.getByText(waitingSession.waiting!)).toBeVisible();
    await expect(inspector.getByText("Subagents")).toBeVisible();
    await userEvent.click(inspector.getByRole("button", { name: "Reply" }));
    await expect(args.onReply).toHaveBeenCalledWith(waitingSession);
    await userEvent.click(inspector.getByRole("button", { name: "Open" }));
    await expect(args.onOpen).toHaveBeenCalledWith(waitingSession);
    await userEvent.click(inspector.getByRole("button", { name: /Review the changelog wording/ }));
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "session", id: "118", machine: "studio" });
    await expect(inspector.getByRole("heading", { name: "Review the changelog wording" })).toBeVisible();
    await userEvent.unhover(inspector.getByRole("heading", { name: "Review the changelog wording" }));
  },
};

export const TreeMode: Story = {
  args: { defaultMode: "tree" },
  parameters: { docs: { description: { story: "The same fleet as a table: machines expand into projects into sessions, with subagents nested under their parents. Every row and action is a real control." } } },
  play: async ({ canvas, args }) => {
    await expect(canvas.getByRole("table", { name: "Fleet tree" })).toBeVisible();
    await expect(canvas.getByRole("button", { name: "Fix the flaky lifeline test" })).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Collapse studio" }));
    await expect(canvas.queryByRole("button", { name: "Fix the flaky lifeline test" })).toBeNull();
    await userEvent.click(canvas.getByRole("button", { name: "Expand studio" }));
    await userEvent.click(canvas.getByRole("button", { name: "Review the changelog wording" }));
    await expect(args.onSelect).toHaveBeenCalledWith({ kind: "session", id: "118", machine: "studio" });
    await expect(within(canvas.getByRole("group", { name: "Inspector" })).getByRole("heading", { name: "Review the changelog wording" })).toBeVisible();
    // The toggle goes both ways; the story ends in Tree mode for review.
    await userEvent.click(canvas.getByRole("radio", { name: "Flow" }));
    await waitFor(() => expect(canvas.getByRole("img", { name: /Fleet flow/ })).toBeVisible());
    await userEvent.click(canvas.getByRole("radio", { name: "Tree" }));
    await waitFor(() => expect(canvas.getByRole("table", { name: "Fleet tree" })).toBeVisible());
    await userEvent.unhover(canvas.getByRole("radio", { name: "Tree" }));
  },
};

export const AsTheServiceSendsIt: Story = {
  name: "As the service sends it",
  args: { ...bare, strips: undefined },
  parameters: { docs: { description: { story: "What the service knows today: no turns, no files edited, no start times and no strips. The view draws them absent rather than empty." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("img", { name: /Fleet flow/ })).toBeVisible();
    await expect(canvas.queryByRole("img", { name: /last 30 polls/ })).toBeNull();
  },
};

export const LaptopWidth: Story = {
  name: "Laptop width",
  globals: { viewport: { value: "laptop", isRotated: false } },
  args: { defaultSelected: { kind: "session", id: waitingSession.id, machine: waitingSession.machine } },
  parameters: { docs: { description: { story: "At 1024 px the inspector moves below the chart, which keeps its width." } } },
  play: async ({ canvas }) => {
    const chart = canvas.getByRole("img", { name: /Fleet flow/ });
    const inspector = canvas.getByRole("group", { name: "Inspector" });
    await expect(inspector.getBoundingClientRect().top).toBeGreaterThan(chart.getBoundingClientRect().top);
  },
};

export const EmptyFleet: Story = {
  args: empty,
  parameters: { docs: { description: { story: "No machines yet: a short title and how the fleet appears." } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText("No machines yet")).toBeVisible();
    await expect(canvas.getByText("0 machines · 0 projects · 0 sessions")).toBeVisible();
  },
};
