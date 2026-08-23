import type { APIRoute } from "astro";
import {
  LICENSE_NAME,
  LICENSE_URL,
  MCP_TOOLS,
  PROJECT_NAME,
  PROJECT_SUMMARY,
  PROJECT_TAGLINE,
  REPOSITORY_URL,
  absoluteUrl,
  docPathname,
  loadDocsInOrder,
  rawPathname,
} from "./_seo";

export const prerender = true;

/** A thematic break, blank-line padded so it can never be read as a setext heading. */
const DELIMITER = "\n\n---\n\n";

/**
 * The whole documentation in one fetch, in sidebar order, so an agent ingests it
 * instead of crawling thirty pages.
 *
 * Bodies are emitted exactly as written. The pages quote real tool responses and
 * real terminal output inside fenced blocks, and reflowing any of that would turn
 * documentation into an approximation of it. The content-layer loader already
 * hands back the body with the frontmatter parsed off, so there is nothing to
 * strip; the title and the description are re-emitted above each page instead.
 */
export const GET: APIRoute = async ({ site }) => {
  const [entries, releases] = await Promise.all([
    loadDocsInOrder(),
    import("astro:content").then((m) => m.getCollection("releases")),
  ]);
  releases.sort((a, b) => b.data.date.valueOf() - a.data.date.valueOf());

  const header = [
    `# ${PROJECT_NAME} — complete documentation`,
    "",
    `> ${PROJECT_TAGLINE}`,
    "",
    PROJECT_SUMMARY,
    "",
    `Generated ${new Date().toISOString().slice(0, 10)}. ${entries.length} pages, in the order the site navigation presents them.`,
    `Includes ${releases.length} releases with notes.`,
    `Licensed ${LICENSE_NAME} (${LICENSE_URL}). Source: ${REPOSITORY_URL}`,
    `The server registers ${MCP_TOOLS.length} tools over stdio: ${MCP_TOOLS.join(", ")}.`,
    `Index of the same pages, one link each: ${absoluteUrl(site, "/llms.txt")}`,
  ].join("\n");

  const pages = entries.map((entry) => {
    const html = absoluteUrl(site, docPathname(entry.id));
    const markdown = absoluteUrl(site, rawPathname(entry.id));
    return [
      `# ${entry.data.title}`,
      "",
      `URL: ${html}`,
      `Markdown: ${markdown}`,
      "",
      (entry.body ?? "").trim(),
    ].join("\n");
  });

  const releasesPage = [
    `# Releases`,
    "",
    `URL: ${absoluteUrl(site, "/releases/")}`,
    "",
    releases
      .map(
        (r) =>
          `## ${r.data.version} (${r.data.date.toISOString().split("T")[0]})\nRequires reindex: ${
            r.data.requires_reindex
          }\n\n${(r.body ?? "").trim()}`,
      )
      .join("\n\n"),
  ].join("\n");
  const body = `${[header, ...pages, releasesPage].join(DELIMITER)}\n`;

  return new Response(body, {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
