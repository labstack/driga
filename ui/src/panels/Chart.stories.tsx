import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { panelOf } from "../fixtures/fragments";
import { Chart } from "./Chart";
import { bar, timeseries } from "./charts";

const ts = panelOf("all", "timeseries");
const b = panelOf("all", "bar");

/** Driga's one ECharts wrapper: charts draw from the theme's tokens in
 * either scheme and carry an accessible description. */
const meta = {
  id: "components-chart",
  title: "Components/Chart",
  component: Chart,
  args: { option: t => timeseries(ts.result.frame!, ts.panel, t), label: "Machine memory used", height: 240 },
} satisfies Meta<typeof Chart>;
export default meta;
type Story = StoryObj<typeof meta>;

export const TimeSeries: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Machine memory used" })).toBeVisible(); } };
export const Bars: Story = { args: { option: t => bar(b.result.frame!, b.panel, t), label: "Memory by session", height: 240 } };
export const Small: Story = { args: { small: true, height: 40 } };
