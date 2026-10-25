import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The app's own dashboard host, served by the Go web server from embedded files.
export default defineConfig({
  plugins: [react()],
  base: "/app/",
  publicDir: false,
  build: { outDir: "../internal/web/app", emptyOutDir: true, cssMinify: true, minify: true, rollupOptions: { input: "index.html" } },
});
