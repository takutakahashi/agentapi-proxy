import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Tauri dev server convention: fixed port so the Rust shell can find the app.
const TAURI_PORT = 1420;

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  // Prevent Vite from spawning a browser; Tauri owns the window.
  clearScreen: false,
  server: {
    port: TAURI_PORT,
    strictPort: true,
    host: "127.0.0.1",
  },
  envPrefix: ["VITE_", "TAURI_"],
  build: {
    target: "es2021",
    minify: "esbuild",
    sourcemap: false,
  },
});