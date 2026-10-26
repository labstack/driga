import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, waitFor, within } from "storybook/test";
import { ChartCard } from "./ChartCard";
import { chart } from "./ChartCard-fixtures";

/** A chart with a header, a range, a menu and annotations: the card every chart panel is. */
const meta: Meta<typeof ChartCard> = {
  id: "widgets-chart-card",
  title: "Components/Chart card",
  component: ChartCard,
  args: { ...chart("24h"), onRange: fn(), actions: { table: fn(), ask: fn(), remove: fn() } },
  decorators: [Story => <Box maw={760}><Story /></Box>],
  parameters: { docs: { description: { component: "A header (title, info, a mono caption such as \"25 GB now · peak 30 GB\", the 1h/6h/24h range when the data has one, and a ⋯ menu) over a line with a faded area. The annotations are the premium part: a dashed line at the peak, labelled at its start; yellow bands where a session waited on you, labelled along their bottom; and a labelled mark above the line where Driga killed three processes, so the three kinds of label never share an edge. The tooltip sits on the inset surface with dot, name and a right-aligned mono value. Empty keeps the axes and says why; Loading is a skeleton of the chart's own shape; Error offers a retry and never shows a stale value. Compare Day and Two series for the one-series area against two lines with a legend under the axis." } } },
};
export default meta;
type Story = StoryObj<typeof meta>;

export const Day: Story = {
  play: async ({ args, canvas, userEvent, step }) => {
    await expect(canvas.getByRole("heading", { name: "Memory on studio" })).toBeVisible();
    await expect(canvas.getByRole("img", { name: /Memory on studio/ })).toBeInTheDocument();
    await step("Pick six hours", async () => {
      await userEvent.click(canvas.getByRole("radio", { name: "6h" }));
      await expect(args.onRange).toHaveBeenCalledWith("6h");
    });
  },
};

export const SixHours: Story = { args: chart("6h"), play: async ({ canvas }) => { await expect(canvas.getByRole("radio", { name: "6h" })).toBeChecked(); } };

export const Hour: Story = { args: chart("1h"), play: async ({ canvas }) => { await expect(canvas.getByRole("radio", { name: "1h" })).toBeChecked(); } };

export const TwoSeries: Story = {
  args: chart("6h", true),
  parameters: { docs: { description: { story: "Used memory against the sessions' summed RSS, with the legend under the axis in room the grid leaves for it." } } },
  play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: /Memory on studio/ })).toBeInTheDocument(); },
};

export const FixedWindow: Story = {
  name: "Fixed window",
  args: { ...chart("1h"), range: undefined, onRange: undefined },
  parameters: { docs: { description: { story: "A chart over one fixed window, as the activity source gives the last hour: no range control." } } },
  play: async ({ canvas }) => { await expect(canvas.queryByRole("radio")).toBeNull(); },
};

export const Menu: Story = {
  play: async ({ args, canvas, userEvent }) => {
    await userEvent.click(canvas.getByRole("button", { name: "More for Memory on studio" }));
    const body = within(document.body);
    await userEvent.click(await body.findByRole("menuitem", { name: "Show as table" }));
    await expect(args.actions!.table).toHaveBeenCalled();
    await waitFor(() => expect(body.queryByRole("menu")).toBeNull());
  },
};

export const Loading: Story = { args: { state: "loading" }, play: async ({ canvas }) => { await expect(canvas.getByLabelText("Loading chart")).toBeInTheDocument(); } };

export const Empty: Story = {
  args: { ...chart("1h"), state: "empty" },
  play: async ({ canvas }) => { await expect(canvas.getByText("no samples in the last 1h")).toBeVisible(); },
};

export const Failed: Story = {
  args: { state: "error", message: "The collector on studio did not answer.", onRetry: fn() },
  play: async ({ args, canvas, userEvent }) => {
    await expect(canvas.getByRole("alert")).toHaveTextContent("The collector on studio did not answer.");
    await userEvent.click(canvas.getByRole("button", { name: "Retry" }));
    await expect(args.onRetry).toHaveBeenCalled();
  },
};
