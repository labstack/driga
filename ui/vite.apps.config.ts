import react from "@vitejs/plugin-react";
import license from "rollup-plugin-license";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

// The MCP App is one self-contained HTML file: hosts load it in a sandboxed
// iframe whose CSP allows no network, so every script, style and font is inline.
export default defineConfig({
  plugins: [react(), license({ thirdParty: { output: { file: "../internal/mcp/apps/THIRD-PARTY.txt" } } }), viteSingleFile()],
  publicDir: false,
  build: { outDir: "../internal/mcp/apps", emptyOutDir: true, assetsInlineLimit: 100_000, cssMinify: true, minify: true, rollupOptions: { input: "panels.html" } },
});
