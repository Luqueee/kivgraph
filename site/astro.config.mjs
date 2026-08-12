import node from "@astrojs/node";
import starlight from "@astrojs/starlight";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

// pm2 runs one Node process. Every route is prerendered, so the standalone
// server only serves files and the 404 route; `output: "server"` is what makes
// the adapter emit that entry point at all.
export default defineConfig({
  site: process.env.LADYGRAPH_SITE_URL ?? "http://localhost:6767",
  output: "server",
  adapter: node({ mode: "standalone" }),
  // 6767 everywhere: `astro dev`, `astro preview` and the standalone server
  // the pm2 unit starts all answer on the same port, so a local check and the
  // deployed site are never two different addresses.
  server: { port: 6767, host: true },
  integrations: [
    starlight({
      title: "Ladygraph",
      description:
        "A canonical code graph for Go, TypeScript and Rust, served over MCP.",
      favicon: "/favicon.svg",
      customCss: ["./src/styles/global.css"],
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/Luqueee/ladygraph",
        },
      ],
      sidebar: [
        {
          label: "Start here",
          items: [
            { label: "Install", slug: "install" },
            { label: "Quickstart", slug: "quickstart" },
            { label: "MCP clients", slug: "mcp-clients" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Indexing", slug: "guides/indexing" },
            { label: "Graph viewer", slug: "guides/viewer" },
            { label: "Maintenance", slug: "guides/maintenance" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "CLI", slug: "reference/cli" },
            { label: "MCP tools", slug: "reference/mcp-tools" },
            { label: "Configuration", slug: "reference/configuration" },
            { label: "Resolution vocabulary", slug: "reference/resolution" },
          ],
        },
        { label: "Limits", slug: "limits" },
      ],
    }),
  ],
  vite: { plugins: [tailwindcss()] },
});
