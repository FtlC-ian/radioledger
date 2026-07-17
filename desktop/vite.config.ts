import { defineConfig } from "vite";
import { resolve } from "path";

export default defineConfig({
  // Vite options tailored for Tauri development.
  clearScreen: false,
  server: {
    port: 6173,
    strictPort: true,
    watch: {
      ignored: ["**/src-tauri/**"],
    },
  },
  envPrefix: ["VITE_", "TAURI_"],
  build: {
    // Tauri supports ES2021
    target: process.env.TAURI_PLATFORM == "windows" ? "chrome105" : "safari13",
    minify: !process.env.TAURI_DEBUG ? "esbuild" : false,
    sourcemap: !!process.env.TAURI_DEBUG,
    rollupOptions: {
      input: resolve(__dirname, "src/index.html"),
    },
  },
  root: "src",
  publicDir: "../public",
});
