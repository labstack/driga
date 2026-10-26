import react from "@vitejs/plugin-react";
import license from "rollup-plugin-license";
import { defineConfig } from "vite";

// The app's own dashboard host, served by the Go web server from embedded files.
export default defineConfig({
  plugins: [react(), license({ thirdParty: { output: { file: "../internal/web/app/THIRD-PARTY.txt" } } })],
  base: "/app/",
  publicDir: false,
  // just dev: Vite serves the page with hot reload and sends the API to
  // driga dev serve, which answers it as the app does.
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: Object.fromEntries(["/app/api", "/app/action", "/app/confirm", "/app/open-external", "/chat", "/static"].map(path => [path, "http://127.0.0.1:47801"])),
  },
  build: { outDir: "../internal/web/app", emptyOutDir: true, cssMinify: true, minify: true, rollupOptions: { input: "index.html" } },
});
