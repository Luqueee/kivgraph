import { existsSync } from "node:fs";
import node from "@astrojs/node";
import { unified } from "@astrojs/markdown-remark";
import sitemap from "@astrojs/sitemap";
import starlight from "@astrojs/starlight";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";
import rehypeExternalLinks from "rehype-external-links";
import { lastmodFor } from "./src/lastmod.mjs";
import { PROJECT_NAME, PROJECT_TAGLINE } from "./src/site.mjs";

// The deployment declares itself in `landing/.env`, and this file has to read
// it before Vite does: the config is not a module Vite transforms, so a bare
// `process.env` sees only what the shell exported. `loadEnvFile` puts the file
// into `process.env`, which is also what feeds `import.meta.env` in the
// components, so `site` and the analytics tracker cannot end up half
// configured. It throws when the file is absent, and a variable already
// exported wins over the file.
if (existsSync(".env")) {
  process.loadEnvFile();
}

// pm2 runs one Node process. Every route is prerendered, so the standalone
// server only serves files and the 404 route; `output: "server"` is what makes
// the adapter emit that entry point at all.
export default defineConfig({
  // `site` is baked in at build time and every canonical, `og:url`, sitemap
  // entry and `llms.txt` link derives from it, so the fallback has to be the
  // real origin: a deploy that forgot the variable once published
  // `http://localhost:6767` as the canonical of every page, and CI builds with
  // no `.env` at all. Development is the case that overrides it, with
  // `KIVGRAPH_LANDING_URL=http://localhost:6767` in `landing/.env`.
  //
  // The origin is the apex of `kivgraph.dev`, which is the form
  // `internal/supervisor/supervisor_linux.go` already writes into the
  // `Documentation=` line of every systemd unit it generates. `www` is not a
  // second origin: the host redirects it here rather than serving it, because
  // two hostnames answering the same HTML is the duplicate every canonical on
  // this site exists to prevent.
  site: process.env.KIVGRAPH_LANDING_URL ?? "https://kivgraph.dev",
  output: "server",
  adapter: node({ mode: "standalone" }),
  // Every canonical this site emits carries a trailing slash, and the default
  // `ignore` served the other form too: `/docs/cli` and `/docs/cli/` both
  // answered `200` with the same HTML and the same canonical. That is a second
  // URL per page for a crawler to fetch and then discard -- 37 of them --
  // which is exactly the crawl budget a young property does not have to spare.
  // `always` makes the slashless form redirect instead. Astro warns that
  // prerendered pages are the host's business rather than its own, so whether
  // this reaches them is measured, not assumed; the figure is in `AGENTS.md`.
  trailingSlash: "always",
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
  //
  // The plugin hangs off an explicit `unified()` processor rather than the bare
  // `markdown.rehypePlugins` array, which Astro 7 deprecated: it still coerces
  // the old shape onto whatever processor is configured, and warns once per
  // build while doing it. Naming the processor is also what keeps Starlight
  // working -- it does not set one, it *mutates* the configured one, pushing its
  // asides, heading anchors and RTL code support into `remarkPlugins` and
  // `rehypePlugins`, and it only recognises two engines. A processor it cannot
  // recognise is not an error: it warns and silently drops those transforms.
  markdown: {
    processor: unified({
      rehypePlugins: [
        [
          rehypeExternalLinks,
          { target: "_blank", rel: ["noopener", "noreferrer"] },
        ],
      ],
    }),
  },
  // The docs namespace was renamed `reference` -> `docs`, and Google had
  // already discovered the old URLs. The target carries the trailing slash on
  // purpose: every canonical on this site has one, and `/docs/[...slug]`
  // without it resolves to a page whose own `<link rel="canonical">` points
  // somewhere else -- a 301 that lands on a non-canonical URL, which is one
  // more hop than a crawler needs to be given.
  redirects: {
    "/reference/[...slug]": "/docs/[...slug]/",
  },
  integrations: [
    // Declared here so Starlight does not add its own: it checks
    // `config.integrations` for `@astrojs/sitemap` and only falls back to its
    // wrapper when nobody named it. The wrapper forwards nothing but `i18n`
    // -- `node_modules/@astrojs/starlight/integrations/sitemap.ts` -- so a
    // `lastmod` has to come from here. `serialize` omits the field for a URL
    // whose date cannot be established rather than inventing one; see
    // `src/lastmod.mjs` for why that is the only other acceptable answer.
    sitemap({
      serialize(item) {
        const lastmod = lastmodFor(item.url);
        return lastmod === undefined ? item : { ...item, lastmod };
      },
    }),
    starlight({
      // The name and the sentence come from `src/site.mjs` because this file
      // cannot import `src/pages/_seo.ts` -- that one imports `astro:content`,
      // which does not exist until the build runs. Written here as literals
      // they were a second, different tagline.
      title: PROJECT_NAME,
      description: PROJECT_TAGLINE,
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
                { label: "find_by_intent", slug: "docs/tools/find-by-intent" },
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
        { label: "Benchmark", slug: "comparison" },
        { label: "Limits", slug: "limits" },
        // `/releases/` is a `<StarlightPage>` rather than a collection entry, so
        // it takes a `link` and not a `slug`. It is here because the footer of
        // the landing was its only inbound link -- one, from the homepage --
        // while every collection page collects 37 or more from this sidebar.
        { label: "Releases", link: "/releases/" },
      ],
    }),
  ],
  vite: { plugins: [tailwindcss()] },
});
