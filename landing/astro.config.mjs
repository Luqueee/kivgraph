import { existsSync } from "node:fs";
import node from "@astrojs/node";
import starlight from "@astrojs/starlight";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";
import rehypeExternalLinks from "rehype-external-links";

// The deployment declares itself in `landing/.env`, and this file has to read
// it before Vite does: the config is not a module Vite transforms, so a bare
// `process.env` sees only what the shell exported -- which is how a deploy that
// forgot to export one variable published `http://localhost:6767` as the
// canonical of every page. `loadEnvFile` puts the file into `process.env`,
// which is also what feeds `import.meta.env` in the components, so `site` and
// the analytics tracker cannot end up half configured. It throws when the file
// is absent, and a variable already exported wins over the file.
if (existsSync(".env")) {
  process.loadEnvFile();
}

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
  // Every link that leaves the site opens in a new tab. This covers the
  // documentation's markdown, where an author cannot be expected to spell the
  // attributes out on every link; the landing's own anchors and the social icon
  // are components, which no rehype pass can reach, so they carry them
  // literally. `noopener` is the point of the `rel`: without it the opened tab
  // gets a handle on `window.opener` and can navigate this one.
  markdown: {
    rehypePlugins: [
      [
        rehypeExternalLinks,
        { target: "_blank", rel: ["noopener", "noreferrer"] },
      ],
    ],
  },
  redirects: {
    "/reference/[...slug]": "/docs/[...slug]",
  },
  integrations: [
    starlight({
      title: "Kivgraph",
      description:
        "A canonical code graph for Go, TypeScript, Rust, Python and Dart, served over MCP.",
      // The favicon is an SVG wrapper around the committed raster mark. Its
      // rounded clip is subtle, so the tab icon keeps the square canvas while
      // avoiding sharp corners.
      favicon: "/favicon.svg",
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
        SocialIcons: "./src/components/starlight/SocialIcons.astro",
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
            { label: "Claude Code", slug: "mcp/claude-code" },
            { label: "Codex", slug: "mcp/codex" },
            { label: "Oh My Pi", slug: "mcp/oh-my-pi" },
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
            { label: "Code intelligence", slug: "code-intelligence" },
            {
              label: "Repository relationships",
              slug: "repository-relationships",
            },
            {
              label: "Token-efficient code",
              slug: "token-efficient-code-understanding",
            },
            {
              label: "Cross-repository code graph",
              slug: "cross-repository-code-graph",
            },
            {
              label: "Workspace code intelligence",
              slug: "workspace-code-intelligence",
            },
            { label: "Kivgraph FAQ", slug: "kivgraph-faq" },
          ],
        },
        {
          label: "Docs",
          items: [
            { label: "CLI", slug: "docs/cli" },
            { label: "MCP tools", slug: "docs/mcp-tools" },
            {
              // Explicit and grouped by what the tool is for. Alphabetical
              // autogeneration would scatter the traversal tools among the
              // lookups, and the order is the routing advice.
              label: "Tools",
              collapsed: true,
              items: [
                { label: "find_symbol", slug: "docs/tools/find-symbol" },
                { label: "get_symbol", slug: "docs/tools/get-symbol" },
                { label: "get_source", slug: "docs/tools/get-source" },
                {
                  label: "get_file_outline",
                  slug: "docs/tools/get-file-outline",
                },
                {
                  label: "find_references",
                  slug: "docs/tools/find-references",
                },
                {
                  label: "find_cross_repo_consumers",
                  slug: "docs/tools/find-cross-repo-consumers",
                },
                {
                  label: "trace_dependencies",
                  slug: "docs/tools/trace-dependencies",
                },
                {
                  label: "get_blast_radius",
                  slug: "docs/tools/get-blast-radius",
                },
                {
                  label: "list_repositories",
                  slug: "docs/tools/list-repositories",
                },
                { label: "graph_status", slug: "docs/tools/graph-status" },
                {
                  label: "index_project",
                  slug: "docs/tools/index-project",
                },
              ],
            },
            { label: "Configuration", slug: "docs/configuration" },
            { label: "Resolution vocabulary", slug: "docs/resolution" },
          ],
        },
        { label: "Compared", slug: "comparison" },
        { label: "Limits", slug: "limits" },
      ],
    }),
  ],
  vite: { plugins: [tailwindcss()] },
});
