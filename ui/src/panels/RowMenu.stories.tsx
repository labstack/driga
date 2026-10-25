import { Group, Text } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, screen, userEvent, waitFor } from "storybook/test";
import { RowMenu } from "./RowMenu";

/** A row's other actions behind ⋯: the main one runs on a click of the
 * row. Destructive ones are red and still ask before they act. */
const meta = {
  id: "components-row-menu",
  title: "Components/Row menu",
  component: RowMenu,
  args: { kinds: ["jump", "stop"], row: { id: 12, title: "Fix the flaky checkout test" }, act: fn(async () => undefined), label: "Fix the flaky checkout test" },
  decorators: [Story => <Group className="row-hover" justify="space-between" w={420} p="xs"><Text size="sm">Fix the flaky checkout test</Text><Story /></Group>],
} satisfies Meta<typeof RowMenu>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Session: Story = {
  play: async ({ canvas, args }) => {
    await userEvent.click(canvas.getByRole("button", { name: "Actions for Fix the flaky checkout test" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Stop" }));
    await expect(args.act).toHaveBeenCalledWith({ kind: "stop", row: args.row });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  },
};
export const ProcessTree: Story = { args: { kinds: ["kill", "force_kill"], label: "vite dev server" } };
export const Cleanup: Story = { args: { kinds: ["cleanup"], label: "/home/dev/src/harbor-wt/fix-cart" } };
export const Review: Story = { args: { kinds: ["jump", "ack"] } };
export const Nothing: Story = { args: { kinds: [] } };
