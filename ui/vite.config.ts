import react from "@vitejs/plugin-react";
import license from "rollup-plugin-license";
import { defineConfig } from "vite";

// The app's own dashboard host, served by the Go web server from embedded files.
export default defineConfig({
  plugins: [react(), license({ thirdParty: { output: { file: "../internal/web/app/THIRD-PARTY.txt" } } })],
  base: "/app/",
  publicDir: false,
  build: { outDir: "../internal/web/app", emptyOutDir: true, cssMinify: true, minify: true, rollupOptions: { input: "index.html" } },
});
