import { Stack, Text, ThemeIcon } from "@mantine/core";
import { Desktop } from "@phosphor-icons/react";

/** FleetEmpty is the view with no machines: a short title and what will
 * appear and how, as Canon's empty state asks. */
export function FleetEmpty() {
  return <Stack align="center" justify="center" gap={6} style={{ flex: 1 }} py="xl">
    <ThemeIcon variant="light" color="gray" radius="xl" size={36}><Desktop size={18} aria-hidden /></ThemeIcon>
    <Text fw={600} fz="md">No machines yet</Text>
    <Text fz="sm" c="dimmed" ta="center" maw={420} style={{ textWrap: "pretty" }}>Start an agent in a terminal on this machine, or pair another one; the fleet appears here within a few seconds.</Text>
  </Stack>;
}
