import { Box } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { panelOf } from "../fixtures/fragments";
import { HostContext } from "./host";
import { PanelView } from "./PanelView";

const act = fn(async () => undefined);
const home = (id: string) => panelOf("home", id);
const quiet = (id: string) => panelOf("home_quiet", id);

/** Home's own panels, each as the default Home defines it, over the busy
 * fleet and over a quiet one. */
const meta = {
  id: "components-home-panels",
  title: "Components/Home panels",
  component: PanelView,
  args: { ...home("inbox"), act },
  decorators: [Story => <HostContext.Provider value={{ act, local: "studio-id" }}><Box w={960}><Story /></Box></HostContext.Provider>],
} satisfies Meta<typeof PanelView>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Inbox: Story = { play: async ({ canvas }) => { await expect(canvas.getByText(/Keep the old webhook endpoint during the cutover/)).toBeVisible(); } };
export const LeftoverProcessTrees: Story = { args: home("orphans"), play: async ({ canvas }) => { await expect(canvas.getByText("vite dev server")).toBeVisible(); } };
export const QuietLongerThanExpected: Story = { args: home("stuck"), play: async ({ canvas }) => { await expect(canvas.getByText("busy, no output for 34m, CPU idle")).toBeVisible(); } };
export const Sessions: Story = { args: home("sessions") };
export const MemoryByProject: Story = { args: home("memory"), play: async ({ canvas }) => { await expect(canvas.getByText(/harbor most/)).toBeVisible(); } };
export const MachineMemoryUsed: Story = { args: home("machine_memory") };
export const InboxEmpty: Story = { args: quiet("inbox"), play: async ({ canvas }) => { await expect(canvas.getByText(/Nothing needs you/)).toBeVisible(); } };
export const SessionsEmpty: Story = { args: quiet("sessions"), play: async ({ canvas }) => { await expect(canvas.getByText(/No live sessions/)).toBeVisible(); } };
