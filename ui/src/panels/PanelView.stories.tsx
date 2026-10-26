import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor, within } from "storybook/test";
import { editPrompt, prompt } from "../fixtures/chat";
import { panelOf } from "../fixtures/fragments";
import { HostContext } from "./host";
import { PanelView } from "./PanelView";

const one = (id: string) => panelOf("all", id);
const act = fn(async () => undefined);

/** One panel of each kind, as dashboards and Chat cards show them, over
 * the fictional fleet. Every kind also has loading, empty, error and narrow
 * states. */
const meta = {
  id: "components-panel",
  title: "Components/Panel",
  component: PanelView,
  args: { ...one("stat"), act },
  decorators: [(Story, { parameters }) => <HostContext.Provider value={{ act, local: "studio-id", ...parameters.host }}>
    <Box w={parameters.width ?? 720} h={parameters.height}><Story /></Box>
  </HostContext.Provider>],
  parameters: { docs: { description: { component: "A panel is a card: its title, an optional description behind the info icon, a summary line and a table view for charts, and its body in the state its result is in." } } },
} satisfies Meta<typeof PanelView>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Stat: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("heading", { name: "Live sessions" })).toBeVisible(); } };
export const StatOutOfTotal: Story = { args: one("stat_total"), play: async ({ canvas }) => { await expect(canvas.getByText(/\/ 32/)).toBeVisible(); } };
export const Gauge: Story = { args: one("gauge") };
export const Timeseries: Story = { args: one("timeseries"), play: async ({ canvas }) => { await expect(canvas.getByText(/highest now/)).toBeVisible(); } };
export const TimeseriesOneLine: Story = { args: one("timeseries_one"), play: async ({ canvas }) => { await expect(canvas.getByText(/now, peak/)).toBeVisible(); } };
export const TimeseriesAsTable: Story = {
  args: one("timeseries_one"),
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole("button", { name: "More for Swap used on studio" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Show as table" }));
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    await expect(canvas.getByRole("columnheader", { name: /Time/ })).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "More for Swap used on studio" }));
    await expect(await screen.findByRole("menuitem", { name: "Show as chart" })).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  },
};
export const Bar: Story = { args: one("bar"), play: async ({ canvas }) => { await expect(canvas.getByText(/Fix the flaky checkout test #12 most/)).toBeVisible(); } };
export const BarByProject: Story = { args: one("bar_project") };
/** More rows than the card holds: the list scrolls and its scrollbar sits
 * beside the values, not over them. */
export const BarOverflowing: Story = {
  args: (() => {
    const panel = one("bar_project");
    const rows = panel.result.frame?.rows ?? [];
    const many = Array.from({ length: 12 }, (_, i) => [`${rows[i % rows.length]?.[0] ?? "project"}-${i}`, 1_000_000_000 * (12 - i)]);
    return { ...panel, result: { ...panel.result, frame: { ...panel.result.frame!, rows: many } }, fill: true };
  })(),
  parameters: { height: 260 },
  play: async ({ canvas, canvasElement }) => {
    await expect(canvas.getAllByRole("listitem")).toHaveLength(12);
    // The list overflows, so the card draws its scrollbar.
    await waitFor(() => expect(canvasElement.querySelector(".mantine-ScrollArea-scrollbar[data-orientation=vertical]")).toBeVisible());
  },
};
export const Table: Story = {
  args: one("table"),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("columnheader", { name: /State/ })).toBeVisible();
    await expect(canvas.getByLabelText("stuck, inferred")).toBeVisible();
  },
};
export const TableSorted: Story = {
  args: one("table"),
  play: async ({ canvas }) => {
    const header = canvas.getByRole("columnheader", { name: /Mem/ });
    await userEvent.click(within(header).getByRole("button"));
    await expect(header).toHaveAttribute("aria-sort", "descending");
  },
};
export const List: Story = { args: one("list") };
export const Inbox: Story = {
  args: one("inbox"),
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/Needs you · 2/)).toBeVisible();
    await expect(canvas.getByText(/Machines · 2/)).toBeVisible();
    await expect(canvas.getByText(/To review · 3/)).toBeVisible();
    await expect(canvas.getByText("clean")).toBeVisible();
    await expect(canvas.getByText("3 uncommitted")).toBeVisible();
  },
};
export const InboxWithPrompts: Story = {
  args: one("inbox"),
  parameters: { host: { prompts: [prompt, editPrompt], answer: fn(async () => undefined) } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/Permission prompts · 2/)).toBeVisible();
    await expect(canvas.getByText("Bash(npm run migrate -- --env staging)")).toBeVisible();
  },
};
export const InboxReviewed: Story = {
  args: one("inbox"),
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole("button", { name: /^Mark reviewed · Fix the flaky checkout test/ }));
    await expect(act).toHaveBeenCalledWith(expect.objectContaining({ kind: "ack" }));
  },
};
export const InboxNeedsYouOnly: Story = {
  name: "Inbox, needs you only",
  args: one("inbox"),
  parameters: { host: { inboxFilter: "waiting" } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/Needs you · 2/)).toBeVisible();
    await expect(canvas.queryByText(/To review/)).toBeNull();
  },
};
export const Cards: Story = { args: one("cards") };
export const Timeline: Story = { args: one("timeline"), play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Last hour" })).toBeInTheDocument(); } };
export const Notes: Story = { args: one("text"), play: async ({ canvas }) => { await expect(canvas.getByRole("cell", { name: "harbor" })).toBeVisible(); } };
export const Empty: Story = { args: one("empty"), play: async ({ canvas }) => { await expect(canvas.getByText("No session looks stuck.")).toBeVisible(); } };
export const Failed: Story = { args: one("error"), play: async ({ canvas }) => { await expect(canvas.getByText(/no_such_column/)).toBeVisible(); } };
export const Loading: Story = { args: { panel: one("table").panel, result: undefined }, play: async ({ canvas }) => { await expect(canvas.getByText("Loading…")).toBeVisible(); } };
export const NarrowTable: Story = { args: one("table"), parameters: { width: 360 } };
export const NarrowChart: Story = { args: one("timeseries"), parameters: { width: 360 } };
export const NarrowInbox: Story = { args: one("inbox"), parameters: { width: 360 } };
