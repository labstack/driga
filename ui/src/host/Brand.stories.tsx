import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { Brand } from "./Brand";

/** Driga's mark and wordmark; the wordmark takes the text color, so it
 * reads in both schemes. */
const meta = { id: "components-brand", title: "Components/Brand", component: Brand } satisfies Meta<typeof Brand>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Mark: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("img", { name: "Driga" })).toBeVisible(); } };
