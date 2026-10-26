import { Box, Stack, Text } from "@mantine/core";
import { FleetSankey } from "./FleetSankey";
import { FleetTopology } from "./FleetTopology";
import type { FleetProps } from "./FleetTypes";

/** FleetCompare stacks the two Fleet variants over the same fleet, each at
 * the window's height, so the owner can weigh them on one page. It exists
 * for the choice; the chosen variant is promoted and this is deleted. */
export function FleetCompare({ height = 820, ...props }: FleetProps & { height?: number }) {
  return <Stack gap="xl">
    <Stack gap="xs">
      <Text fz="xs" fw={600} c="dimmed" tt="uppercase" style={{ letterSpacing: "0.04em" }}>A · Sankey</Text>
      <Box h={height} style={{ display: "flex", flexDirection: "column" }}><FleetSankey {...props} /></Box>
    </Stack>
    <Stack gap="xs">
      <Text fz="xs" fw={600} c="dimmed" tt="uppercase" style={{ letterSpacing: "0.04em" }}>B · Topology</Text>
      <Box h={height} style={{ display: "flex", flexDirection: "column" }}><FleetTopology {...props} /></Box>
    </Stack>
  </Stack>;
}
