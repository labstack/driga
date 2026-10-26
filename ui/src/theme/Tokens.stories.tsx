import { Badge, Box, Button, Group, SimpleGrid, Stack, Text, Title } from "@mantine/core";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect } from "storybook/test";
import { series, surfaces, tones, typeScale } from "./tokens";

const roles = Object.keys(surfaces.light) as (keyof typeof surfaces.light)[];
const variable: Record<string, string> = { ground: "--driga-ground", raised: "--driga-raised", inset: "--driga-inset", hover: "--driga-hover", border: "--driga-border", borderSoft: "--driga-border-soft", controlBorder: "--driga-control-border", text: "--driga-text", muted: "--driga-muted", faint: "--driga-faint" };

function Swatch({ name, css }: { name: string; css: string }) {
  return <Stack gap={4}>
    <Box h={40} style={{ background: css, border: "1px solid var(--driga-border)", borderRadius: "var(--mantine-radius-sm)" }} />
    <Text size="xs">{name}</Text>
  </Stack>;
}

function Tokens() {
  return <Stack gap="lg" maw={960}>
    <Stack gap="xs"><Title order={2} fz="lg">Surfaces and text</Title>
      <SimpleGrid cols={5}>{roles.map(r => <Swatch key={r} name={r} css={`var(${variable[r]})`} />)}</SimpleGrid></Stack>
    <Stack gap="xs"><Title order={2} fz="lg">Tones</Title>
      <Group>{(Object.keys(tones.light) as (keyof typeof tones.light)[]).map(t => <Stack key={t} gap={6} align="flex-start">
        <Button color={t}>{t}</Button><Badge color={t} variant="light">{t}</Badge><Text size="sm" c={t}>{t} text</Text>
      </Stack>)}</Group></Stack>
    <Stack gap="xs"><Title order={2} fz="lg">Chart series</Title>
      <Group>{series.light.map((_, i) => <Box key={i} w={48} h={16} style={{ background: `var(--driga-series-${i}, ${series.light[i]})`, borderRadius: 4 }} aria-hidden />)}</Group>
      <Text size="xs" c="dimmed">Six fixed slots, none a state hue; charts read them per scheme.</Text></Stack>
    <Stack gap="xs"><Title order={2} fz="lg">Type</Title>
      {(Object.entries(typeScale) as [string, number][]).filter(([k]) => k !== "micro").map(([k, px]) => <Text key={k} fz={px}>{k} · {px}px</Text>)}</Stack>
  </Stack>;
}

/** Driga's design tokens in the current scheme: every color comes from the
 * console palette (internal/view), so a palette change shows here at once. */
const meta = { id: "theme-tokens", title: "Theme/Tokens", component: Tokens } satisfies Meta<typeof Tokens>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Everything: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole("button", { name: "danger" })).toBeVisible(); } };
