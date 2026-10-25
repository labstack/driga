import "@mantine/core/styles.css";
import { MantineProvider, type MantineColorScheme } from "@mantine/core";
import { IconContext } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { cssVariables, theme } from "./theme";

const icons = { size: 16, weight: "regular" } as const;

// The app's page carries a per-response nonce its policy allows Mantine's
// style element by; an MCP App's host allows inline styles itself.
function styleNonce(): string {
  return document.querySelector<HTMLMetaElement>('meta[name="driga-style-nonce"]')?.content ?? "";
}

/** Provider applies Driga's theme. The app follows the system scheme, which
 * the desktop app sets from ui.theme; an MCP App follows its host's. */
export function Provider({ scheme, children }: { scheme?: Exclude<MantineColorScheme, "auto">; children: ReactNode }) {
  return <MantineProvider theme={theme} cssVariablesResolver={cssVariables} defaultColorScheme="auto" forceColorScheme={scheme} getStyleNonce={styleNonce}>
    <IconContext.Provider value={icons}>{children}</IconContext.Provider>
  </MantineProvider>;
}
