import path from "node:path";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

const sourceDirectory = path.dirname(fileURLToPath(import.meta.url));

// `ladygraph ui` serves this bundle and /api/v1 from one origin. The dev
// server proxies the same paths to a local `ladygraph ui` so the viewer always
// reads a published HotSnapshot, never a fixture.
const apiTarget = process.env.LADYGRAPH_API ?? "http://127.0.0.1:7777";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(sourceDirectory, "src"),
    },
  },
  server: {
    proxy: {
      "/api": { target: apiTarget, changeOrigin: false },
    },
  },
  preview: {
    proxy: {
      "/api": { target: apiTarget, changeOrigin: false },
    },
  },
});
