import { Text } from "@mantine/core";
import type { Ref } from "react";
import { Board } from "../panels/Board";
import type { Fragment } from "../panels/types";

/** PanelsView is what the MCP App shows in a chat host: the fragment a
 * panel tool returned, or why there is none. */
export function PanelsView({ ref, fragment, problem, disconnected, onVars }: {
  ref?: Ref<HTMLDivElement>; fragment: Fragment | null; problem: string | null; disconnected?: boolean;
  onVars?: (vars: Record<string, string>) => void;
}) {
  return <div ref={ref} role="main" aria-label="Driga panels">
    {problem && <Text size="sm" c="danger" role="alert" px="md" py="xs">{problem}</Text>}
    {fragment ? <Board fragment={fragment} onVars={onVars} compact /> : !problem && <Text size="sm" c="dimmed" p="md">{disconnected ? "This view could not connect." : "Loading…"}</Text>}
  </div>;
}
