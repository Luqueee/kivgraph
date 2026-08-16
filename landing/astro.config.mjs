import node from "@astrojs/node";
import starlight from "@astrojs/starlight";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

// pm2 runs one Node process. Every route is prerendered, so the standalone
// server only serves files and the 404 route; `output: "server"` is what makes
// the adapter emit that entry point at all.
export default defineConfig({
  site: process.env.KIVGRAPH_LANDING_URL ?? "http://localhost:6767",
  output: "server",
  adapter: node({ mode: "standalone" }),
  // 6767 everywhere: `astro dev`, `astro preview` and the standalone server
  // the pm2 unit starts all answer on the same port, so a local check and the
  // deployed landing are never two different addresses.
  server: { port: 6767, host: true },
  integrations: [
    starlight({
      title: "Kivgraph",
      description:
        "A canonical code graph for Go, TypeScript and Rust, served over MCP.",
      // The mark is a raster: there is no favicon.svg to prefer. Starlight
      // emits this one, and Head adds the 16px and the Apple tile.
      favicon: "/favicon-32.png",
      // global.css declares the tokens for both halves of the site; docs.css
      // is the Starlight-only skin that dresses these pages like the landing.
      // Order matters: the skin reads tokens the first file declares.
      customCss: ["./src/styles/global.css", "./src/styles/docs.css"],
      // Dark only, like the graph viewer: the provider pins the attribute and
      // the select has nothing left to offer. Head adds what Starlight omits --
      // structured data, the Twitter card body, the icon set, and the links an
      // agent follows to the markdown source.
      components: {
        Head: "./src/components/starlight/Head.astro",
        ThemeProvider: "./src/components/starlight/ThemeProvider.astro",
        ThemeSelect: "./src/components/starlight/ThemeSelect.astro",
      },
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/Luqueee/kivgraph",
        },
      ],
      sidebar: [
        {
          label: "Start here",
          items: [
            { label: "Install", slug: "install" },
            { label: "Quickstart", slug: "quickstart" },
          ],
        },
        {
          label: "MCP server",
          items: [
            { label: "Clients", slug: "mcp/clients" },
            { label: "Agent Skill", slug: "mcp/skills" },
            { label: "Using it from an agent", slug: "mcp/usage" },
            { label: "Troubleshooting", slug: "mcp/troubleshooting" },
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
          label: "Docs",
          items: [
            { label: "CLI", slug: "reference/cli" },
            { label: "MCP tools", slug: "reference/mcp-tools" },
            {
              // Explicit and grouped by what the tool is for. Alphabetical
              // autogeneration would scatter the traversal tools among the
              // lookups, and the order is the routing advice.
              label: "Tools",
              collapsed: true,
              items: [
                { label: "find_symbol", slug: "reference/tools/find-symbol" },
                { label: "get_symbol", slug: "reference/tools/get-symbol" },
                { label: "get_source", slug: "reference/tools/get-source" },
                {
                  label: "get_file_outline",
                  slug: "reference/tools/get-file-outline",
                },
                {
                  label: "find_references",
                  slug: "reference/tools/find-references",
                },
                {
                  label: "find_cross_repo_consumers",
                  slug: "reference/tools/find-cross-repo-consumers",
                },
                {
                  label: "trace_dependencies",
                  slug: "reference/tools/trace-dependencies",
                },
                {
                  label: "get_blast_radius",
                  slug: "reference/tools/get-blast-radius",
                },
                {
                  label: "list_repositories",
                  slug: "reference/tools/list-repositories",
                },
                { label: "graph_status", slug: "reference/tools/graph-status" },
                {
                  label: "index_project",
                  slug: "reference/tools/index-project",
                },
              ],
            },
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
