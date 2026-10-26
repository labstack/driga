import type { StorybookConfig } from "@storybook/react-vite";

// Local review only: Storybook is never published (Canon design.md).
const config: StorybookConfig = {
  stories: ["../src/**/*.stories.tsx"],
  framework: "@storybook/react-vite",
  core: { disableTelemetry: true },
  features: { sidebarOnboardingChecklist: false, menuOnboardingChecklist: false },
  // The app's brand files, which the Go server serves at /static.
  staticDirs: [{ from: "../../internal/web/static", to: "/static" }],
  addons: ["@storybook/addon-docs", "@storybook/addon-a11y", "@storybook/addon-vitest"],
  viteFinal: vite => ({
    ...vite,
    // The app's build writes notices and serves under /app/; Storybook does neither.
    base: "/",
    plugins: (vite.plugins ?? []).filter(p => !p || typeof p !== "object" || !("name" in p) || p.name !== "rollup-plugin-license"),
    optimizeDeps: { ...vite.optimizeDeps, include: [...(vite.optimizeDeps?.include ?? []), "react", "react-dom", "react-dom/client", "react/jsx-runtime", "react/jsx-dev-runtime", "@mantine/core", "@mantine/hooks", "storybook/internal/core-events", "@storybook/react-vite", "echarts/core", "echarts/charts", "echarts/components", "echarts/renderers", "@tanstack/react-table", "react-markdown", "remark-gfm", "@mantine/notifications", "@mantine/spotlight", "@phosphor-icons/react", "react-grid-layout", "react-resizable-panels", "@modelcontextprotocol/ext-apps"] },
  }),
};

export default config;
