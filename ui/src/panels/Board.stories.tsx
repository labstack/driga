import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn } from "storybook/test";
import { fragments } from "../fixtures/fragments";
import { Board } from "./Board";

/** A fragment's panels on the 12-column grid, as the MCP App and Chat's
 * full-screen view show them. Stacked puts each panel full width, for the
 * Chat column. */
const meta = {
  id: "components-board",
  title: "Components/Board",
  component: Board,
  args: { fragment: fragments.home },
} satisfies Meta<typeof Board>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Home: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("heading", { name: "Home" })).toBeVisible(); } };
export const EveryPanel: Story = { args: { fragment: fragments.all } };
export const WithVariables: Story = { args: { fragment: { ...fragments.home, options: { project: ["atlas", "beacon", "harbor"], machine: ["build-01", "studio"] } }, onVars: fn() } };
export const Compact: Story = { args: { fragment: fragments.home, compact: true } };
export const Stacked: Story = { args: { fragment: fragments.home, compact: true, stacked: true, bare: true }, globals: { viewport: { value: "chat", isRotated: false } } };
