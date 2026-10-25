// The catalog check: every story, its play, accessibility and the layout
// check, in Chromium. Unit tests run from vite.config.ts instead.
import { storybookTest } from "@storybook/addon-vitest/vitest-plugin";
import { playwright } from "@vitest/browser-playwright";
import { defineConfig } from "vitest/config";
import { reviewWidths } from "./.storybook/viewports.ts";

// Two cells keep both schemes and the two window widths without a full
// product of them; Chat's stories pin the Chat column width themselves.
// Storybook's Run tests button sets VITEST_STORYBOOK; there the toolbar's
// globals apply, so it gets one project.
const cells: { colorScheme: string; viewport: keyof typeof reviewWidths }[] | [undefined] = process.env.VITEST_STORYBOOK === "true"
  ? [undefined]
  : [{ colorScheme: "light", viewport: "desktop" }, { colorScheme: "dark", viewport: "laptop" }];

export default defineConfig({
  test: {
    // Shared runners slow stories under load; Canon's timeouts.
    ...(process.env.CI ? { maxWorkers: 1, fileParallelism: false, testTimeout: 60_000 } : { testTimeout: 30_000 }),
    projects: cells.map(cell => ({
      extends: true,
      plugins: [storybookTest({ configDir: ".storybook", ...(cell && { initialGlobals: { colorScheme: cell.colorScheme, viewport: { value: cell.viewport, isRotated: false } } }) })],
      test: {
        name: cell ? `catalog-${cell.colorScheme}-${cell.viewport}` : "catalog",
        browser: { enabled: true, provider: playwright(), headless: true, instances: [{ browser: "chromium" as const }] },
      },
    })),
  },
});
