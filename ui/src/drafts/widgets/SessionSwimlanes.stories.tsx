import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { SessionSwimlanes } from "./SessionSwimlanes";
import { swimlanes } from "./SessionSwimlanes-fixtures";

/** One lane per session, segments colored by state, a line at now. */
const meta = {
  id: "widgets-session-swimlanes",
  title: "Drafts/Widgets/Session swimlanes",
  component: SessionSwimlanes,
  args: swimlanes,
  decorators: [Story => <Box maw={760}><Story /></Box>],
  parameters: { docs: { description: { component: "A Gantt of the last three hours: each lane a session, each segment what it was doing in that state's color, equal neighbours merged so a lane reads as dwell time (Grafana's state timeline), and a line at now. An ECharts custom series; the legend under it gives the words for the colors." } } },
} satisfies Meta<typeof SessionSwimlanes>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ThreeHours: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole("img", { name: "Sessions over the last three hours" })).toBeInTheDocument();
    await expect(canvas.getByLabelText("stuck")).toBeVisible();
  },
};
