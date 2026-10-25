import { Group } from "@mantine/core";

/** Brand is Driga's mark and wordmark from the brand files the app serves.
 * The wordmark is a mask filled with the text color, so it reads in both
 * schemes. */
export function Brand() {
  return <Group gap={8} wrap="nowrap" aria-label="Driga" role="img">
    <img src="/static/brand/murmur.svg" width={24} height={24} alt="" />
    <span className="wordmark" aria-hidden="true" />
  </Group>;
}
