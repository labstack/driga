// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import catppuccin from "@catppuccin/starlight";
import { unified } from "@astrojs/markdown-remark";
import { rehypeTableWrap } from "./src/plugins/rehype-table-wrap.mjs";

// The site URL is what makes llms.txt and the Markdown alternates absolute.
// An agent that resolves a relative link against the wrong origin fetches
// nothing, so this is set rather than inferred.
const SITE = process.env.SITE_URL ?? "https://driga.dev";

export default defineConfig({
  site: SITE,
  trailingSlash: "never",
  build: { format: "file" },
  // A fixed local-development port. strictPort belongs to Vite, not Astro's
  // `server`, so a busy port is an error rather than a silent move elsewhere.
  server: { port: 7431 },
  vite: { server: { strictPort: true } },
  markdown: { processor: unified({ rehypePlugins: [rehypeTableWrap] }) },
  integrations: [
    starlight({
      title: "Driga",
      // The separator between a page's title and the site's: "Install · Driga".
      titleDelimiter: "·",
      description:
        "See what your coding agents are doing, and what needs you. A desktop app, terminal UI and CLI for Claude Code, Codex, Gemini and Copilot sessions.",
      logo: {
        light: "./src/assets/lockup.svg",
        dark: "./src/assets/lockup-white.svg",
        replacesTitle: true,
      },
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/labstack/driga",
        },
      ],
      editLink: {
        baseUrl: "https://github.com/labstack/driga/edit/main/site/",
      },
      // Latte and Mocha are the app's own two themes (`ui.theme`), so the
      // documentation and the product it documents share one palette.
      plugins: [
        catppuccin({
          dark: { flavor: "mocha", accent: "mauve" },
          light: { flavor: "latte", accent: "mauve" },
        }),
      ],
      // Expressive Code's syntax palette is independent of Starlight's tokens;
      // without naming themes the code blocks keep GitHub's colours inside a
      // Catppuccin frame.
      expressiveCode: {
        themes: ["catppuccin-mocha", "catppuccin-latte"],
        styleOverrides: { borderColor: "var(--sl-color-gray-5)" },
      },
      customCss: ["./src/styles/driga.css"],
      components: {
        SocialIcons: "./src/components/SocialIcons.astro",
        // Advertises the Markdown alternate for the current page, so an agent
        // that reads the HTML head never has to parse the HTML body.
        Head: "./src/components/Head.astro",
        // Rendered in place of the title on the landing page, the only page
        // with `hero` frontmatter.
        Hero: "./src/components/landing/Hero.astro",
        // Starlight's own footer, minus the documentation affordances on the
        // splash page.
        Footer: "./src/components/Footer.astro",
        // Adds the site footer band below splash content.
        PageFrame: "./src/components/PageFrame.astro",
      },
      // Starlight derives the canonical URL from the on-disk file name, which
      // under `build.format: "file"` carries a `.html` nothing else uses.
      routeMiddleware: "./src/starlight-route-data.ts",
      lastUpdated: true,
      pagination: true,
      favicon: "/favicon.svg",
      head: [
        { tag: "link", attrs: { rel: "apple-touch-icon", href: "/apple-touch-icon.png" } },
      ],
      sidebar: [
        {
          label: "Start here",
          items: [
            { label: "What Driga is", slug: "start/what-driga-is" },
            { label: "Install", slug: "start/install" },
            { label: "First run", slug: "start/first-run" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Flow", slug: "guides/flow" },
            { label: "Needs you and prompts", slug: "guides/needs-you" },
            { label: "Chat and actions", slug: "guides/chat" },
            { label: "Notifications", slug: "guides/notifications" },
            { label: "Projects and machines", slug: "guides/projects-and-machines" },
            { label: "Paired machines", slug: "guides/fleet" },
            { label: "Cleanup", slug: "guides/cleanup" },
            { label: "Connect an agent over MCP", slug: "guides/mcp" },
            { label: "Windows", slug: "guides/windows" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "CLI", slug: "reference/cli" },
            { label: "Settings", slug: "reference/settings" },
            { label: "MCP tools", slug: "reference/mcp-tools" },
            { label: "Hooks", slug: "reference/hooks" },
          ],
        },
        {
          label: "Explanation",
          items: [
            { label: "How Driga observes sessions", slug: "explanation/how-driga-observes" },
            { label: "Privacy and data", slug: "explanation/privacy-and-data" },
          ],
        },
      ],
    }),
  ],
});
